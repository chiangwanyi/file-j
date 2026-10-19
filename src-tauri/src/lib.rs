// The Rust layer is intentionally thin.
//
// It owns exactly three things:
//   1. creating the native window (Tauri framework responsibility),
//   2. spawning the Go business backend as a sidecar and reading its one-line
//      handshake from stdout,
//   3. exposing that handshake to the WebView and killing the child on exit.
//
// There is no file-system, SSH, transfer or persistence code here. If a feature
// needs behaviour, it belongs in backend/ (Go) or src/ (Vue).

use std::io::{BufRead, BufReader};
use std::path::PathBuf;
use std::process::{Child, Command, Stdio};
use std::sync::Mutex;
use std::time::{Duration, Instant};

use serde::{Deserialize, Serialize};
use tauri::{Manager, RunEvent, State};

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct BackendInfo {
    pub port: u16,
    pub token: String,
    pub pid: u32,
    pub version: String,
    #[serde(rename = "configDir", default)]
    pub config_dir: String,
    #[serde(rename = "logFile", default)]
    pub log_file: String,
}

#[derive(Default)]
pub struct BackendState {
    info: Mutex<Option<BackendInfo>>,
    error: Mutex<Option<String>>,
    child: Mutex<Option<Child>>,
}

/// Returns the WebSocket endpoint and token the front-end must use.
/// Blocks briefly while the Go process starts so the UI can connect on boot.
#[tauri::command]
fn backend_info(state: State<'_, BackendState>) -> Result<BackendInfo, String> {
    let deadline = Instant::now() + Duration::from_secs(20);
    loop {
        if let Some(info) = state.info.lock().unwrap().clone() {
            return Ok(info);
        }
        if let Some(err) = state.error.lock().unwrap().clone() {
            return Err(err);
        }
        if Instant::now() > deadline {
            return Err("后端服务启动超时，请查看日志文件。".into());
        }
        std::thread::sleep(Duration::from_millis(80));
    }
}

/// Locates the Go sidecar in both `tauri dev` and a bundled .app.
fn resolve_sidecar(app: &tauri::AppHandle) -> Result<PathBuf, String> {
    let mut candidates: Vec<PathBuf> = Vec::new();

    // Bundled app: Contents/MacOS/file-jd sits next to the main executable.
    if let Ok(exe) = std::env::current_exe() {
        if let Some(dir) = exe.parent() {
            candidates.push(dir.join("file-jd"));
        }
    }
    // Resource directory (alternative bundling layout).
    if let Ok(dir) = app.path().resource_dir() {
        candidates.push(dir.join("file-jd"));
        candidates.push(dir.join("binaries").join("file-jd"));
    }
    // Development: src-tauri/binaries/file-jd-<target-triple>.
    if cfg!(debug_assertions) {
        let dev_dir = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("binaries");
        if let Ok(entries) = std::fs::read_dir(&dev_dir) {
            for entry in entries.flatten() {
                let name = entry.file_name().to_string_lossy().to_string();
                if name.starts_with("file-jd-") {
                    candidates.push(entry.path());
                }
            }
        }
    }

    for candidate in candidates {
        if candidate.is_file() {
            return Ok(candidate);
        }
    }
    Err("未找到后端可执行文件 file-jd，请先运行 pnpm backend:build".into())
}

fn random_token() -> String {
    // 24 random bytes, hex encoded. The token only has to protect a loopback
    // socket from other local processes, so a CSPRNG-sourced value is ample.
    let mut bytes = [0u8; 24];
    if getrandom::fill(&mut bytes).is_err() {
        // Extremely unlikely; fall back to a time derived value rather than
        // starting without any token.
        let nanos = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .map(|d| d.as_nanos())
            .unwrap_or(0);
        for (i, b) in bytes.iter_mut().enumerate() {
            *b = ((nanos >> (i % 16 * 8)) & 0xff) as u8;
        }
    }
    bytes.iter().map(|b| format!("{:02x}", b)).collect()
}

fn spawn_backend(app: &tauri::AppHandle) -> Result<(), String> {
    let state = app.state::<BackendState>();
    let path = resolve_sidecar(app)?;
    let token = random_token();

    let mut cmd = Command::new(&path);
    cmd.arg("--token")
        .arg(&token)
        .arg("--addr")
        .arg("127.0.0.1:0")
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped());

    let mut child = cmd
        .spawn()
        .map_err(|e| format!("启动后端失败 ({}): {}", path.display(), e))?;

    let stdout = child.stdout.take().ok_or("无法读取后端输出")?;
    let stderr = child.stderr.take();

    // First line on stdout is the handshake; everything after is ignored.
    let mut reader = BufReader::new(stdout);
    let mut line = String::new();
    match reader.read_line(&mut line) {
        Ok(0) => {
            *state.error.lock().unwrap() =
                Some("后端进程未输出握手信息就退出了，请检查日志。".into());
        }
        Ok(_) => match serde_json::from_str::<serde_json::Value>(&line) {
            Ok(value) => {
                let info = BackendInfo {
                    port: value["port"].as_u64().unwrap_or(0) as u16,
                    token: value["token"].as_str().unwrap_or(&token).to_string(),
                    pid: value["pid"].as_u64().unwrap_or(0) as u32,
                    version: value["version"].as_str().unwrap_or("unknown").to_string(),
                    config_dir: value["configDir"].as_str().unwrap_or("").to_string(),
                    log_file: value["logFile"].as_str().unwrap_or("").to_string(),
                };
                if info.port == 0 {
                    *state.error.lock().unwrap() = Some("后端返回了无效端口".into());
                } else {
                    *state.info.lock().unwrap() = Some(info);
                }
            }
            Err(e) => {
                *state.error.lock().unwrap() =
                    Some(format!("解析后端握手失败: {} ({})", e, line.trim()));
            }
        },
        Err(e) => {
            *state.error.lock().unwrap() = Some(format!("读取后端握手失败: {}", e));
        }
    }

    // Keep draining stdout so the child never blocks on a full pipe.
    std::thread::spawn(move || {
        let mut sink = String::new();
        while let Ok(n) = reader.read_line(&mut sink) {
            if n == 0 {
                break;
            }
            eprint!("[backend] {}", sink);
            sink.clear();
        }
    });

    if let Some(err) = stderr {
        std::thread::spawn(move || {
            let reader = BufReader::new(err);
            for line in reader.lines().map_while(Result::ok) {
                eprintln!("[backend] {}", line);
            }
        });
    }

    *state.child.lock().unwrap() = Some(child);
    Ok(())
}

fn shutdown_backend(app: &tauri::AppHandle) {
    let state = app.state::<BackendState>();
    if let Some(mut child) = state.child.lock().unwrap().take() {
        // The backend also watches its parent PID, but ask politely first.
        let _ = child.kill();
        let _ = child.wait();
    }
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    let app = tauri::Builder::default()
        .plugin(tauri_plugin_opener::init())
        .manage(BackendState::default())
        .invoke_handler(tauri::generate_handler![backend_info])
        .setup(|app| {
            if let Err(err) = spawn_backend(app.handle()) {
                eprintln!("[file-j] {}", err);
                *app.state::<BackendState>().error.lock().unwrap() = Some(err);
            }
            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("error while building tauri application");

    app.run(|handle, event| {
        if let RunEvent::Exit = event {
            shutdown_backend(handle);
        }
    });
}
