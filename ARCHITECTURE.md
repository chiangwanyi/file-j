# file-j 架构说明

一款 WinSCP 式双栏文件管理器，界面采用 macOS 风格，支持本地文件操作与 SSH/SFTP 远程传输。

- 前端：Vue 3 + TypeScript + Tailwind CSS
- 外壳：Tauri 2（Rust）
- 业务后端：Go 1.26.9
- 持久化：纯 YAML 文件，无数据库
- 目标系统：macOS 12.0+（验证于 macOS 12.7.6 / Intel）与 Windows 11 x64
- 平台差异一律通过 `//go:build` / `#[cfg]` / 前端 `platform.ts` 适配层隔离，业务代码不含平台分支

---

## 1. 设计原则

1. **业务逻辑只有一份，且在 Go 里。** Rust 不实现任何文件、SSH、传输、配置逻辑，只负责窗口与子进程生命周期。前端只做展示与交互编排。
2. **本地与远程同构。** 本地磁盘与 SFTP 会话实现同一个 `fsx.Provider` 接口，复制/移动/上传/下载共用一套传输引擎，避免四套彼此不一致的代码。
3. **协议即契约。** 前后端只通过一条本机 WebSocket 上的 JSON 消息通信，方法表与错误码集中定义，便于审查与测试。
4. **配置可读、可手改。** 所有持久状态是分域的 YAML 文档，原子写入；密钥不进 YAML。
5. **能力边界写清楚。** 不支持的场景在 UI 与文档中明确说明，而不是留下半成品行为。

---

## 2. 分层与模块职责

```
┌──────────────────────────────────────────────────────────────┐
│  Vue 3 渲染层 (src/)                                          │
│  视图 / 交互 / 选择 / 拖拽 / 排序 / 主题 / 队列展示             │
└───────────────▲──────────────────────────┬───────────────────┘
                │ JSON over WebSocket      │
┌───────────────┴──────────────────────────▼───────────────────┐
│  Go 业务后端 (backend/)  —— 唯一业务实现层                     │
│  ipc · app · fsx · remote · transfer · config · credentials   │
└───────────────▲──────────────────────────────────────────────┘
                │ sidecar: spawn / stdout 握手 / kill
┌───────────────┴──────────────────────────────────────────────┐
│  Tauri 2 壳层 (src-tauri/)  —— 仅框架职责                      │
│  窗口 · 进程生命周期 · 把握手信息交给 WebView                  │
└──────────────────────────────────────────────────────────────┘
```

### 2.1 Rust / Tauri 壳层（`src-tauri/src/lib.rs`，约 440 行）

| 职责 | 说明 |
| --- | --- |
| 创建窗口 | `tauri.conf.json`：1240×800、`titleBarStyle: Overlay`、最小尺寸、CSP |
| 对齐红绿灯 | `center_traffic_lights()`：把系统三色按钮垂直居中到 52px 顶栏中线（AppKit 默认放在 28pt 标题栏里，偏高 12pt）；窗口尺寸/外观变化时重算 |
| 构建菜单栏 | `build_menu()`：全中文原生菜单（`FileJ` / 文件 / 编辑 / 显示 / 窗口 / 帮助），采用 predefined item 以保留系统快捷键 |
| 启动 sidecar | 生成随机 token，`spawn(file-jd --token … --addr 127.0.0.1:0)` |
| 读取握手 | 读 stdout **第一行** JSON（`{port, token, pid, version, configDir}`），后续 stdout 仅转发到日志 |
| 暴露端点 | `#[tauri::command] backend_info()`，前端启动时调用一次 |
| 退出清理 | `RunEvent::Exit` 时 kill 子进程；Go 侧另有父进程消失自检，双保险 |

**明确不做**：没有 `read_dir`、没有 `ssh`、没有 YAML 解析、没有传输逻辑、没有把 Go 的 RPC 逐条代理成 Tauri command。Rust 里唯一的“判断”是找 sidecar 路径；唯一的原生 UI 改动是把系统红绿灯对齐到 52px 顶栏中线。

> 为什么不用 `invoke` 直连业务：若前端调用 Rust command 再由 Rust 转发给 Go，Rust 就会变成业务网关，违反“Rust 只做框架”的约束，也让每次调用多一层无意义转发。

### 2.2 Go 业务后端（`backend/`）

| 包 | 职责 | 关键点 |
| --- | --- | --- |
| `cmd/file-jd` | 进程入口 | 解析参数、初始化日志、绑定端口、**向 stdout 打印唯一一行握手**、信号与父进程退出处理 |
| `internal/ipc` | WebSocket 服务与路由 | token 校验、Origin 校验、每请求一个 goroutine、写串行化、panic 兜底、事件广播 |
| `internal/app` | 组装与 RPC 处理器 | 唯一的“知道所有模块”的地方；路径归一化、参数校验、错误码映射 |
| `internal/fsx` | 文件系统抽象 + 本地实现 | `Provider` 接口；`Local` 基于 `os`；路径、`ls -l` 风格权限串、`~` 展开、废纸篓。平台差异拆到 `paths_*.go` / `os_*.go` / `trash_*.go` |
| `internal/remote` | SSH/SFTP | `x/crypto/ssh` + `pkg/sftp`；连接复用、keepalive、空闲回收、主机密钥策略；`Conn` 实现 `fsx.Provider` |
| `internal/transfer` | 传输引擎 | 队列、并发、递归、进度节流、取消、冲突策略、移动快路径 |
| `internal/config` | YAML 持久化 | 五个文档、原子写、默认值、范围收敛、手改容错 |
| `internal/credentials` | 密钥存储 | macOS Keychain（`security` CLI）/ Windows 凭据管理器（Win32 `CredWriteW`），YAML 只存引用；失败降级为内存 |
| `internal/testutil` | 测试用 SSH/SFTP 服务器 | 仅被 `_test.go` 引用，不进产物 |

### 2.3 第三方库选型

“能复用现成工具就不自己实现”的落地：

| 领域 | 选择 | 原因 |
| --- | --- | --- |
| SSH 协议 | `golang.org/x/crypto/ssh` | Go 官方维护的参考实现，覆盖密钥、agent、keyboard-interactive、keepalive |
| SFTP 协议 | `github.com/pkg/sftp` v1.13 | Go 生态事实标准；并发读写、`PosixRename`、`StatVFS`、超时控制齐全 |
| WebSocket | `github.com/gorilla/websocket` | 最成熟稳定，握手/读写/关闭语义清晰 |
| YAML | `gopkg.in/yaml.v3` | 标准选择，注释与结构体映射稳定 |
| 系统调用 | `golang.org/x/sys/{unix,windows}` | `statfs` / `GetDiskFreeSpaceExW` 取磁盘容量，避免 cgo |
| 前端构建 | Vite 8 + Vue 3.5 + Tailwind 3.4 | 见第 6 节平台约束 |

**没有**自己写 SSH 报文、SFTP 分包、YAML 解析、WebSocket 帧、文件拷贝重试框架。

---

## 3. 启动与交互

### 3.1 启动时序

```
Tauri 进程                      Go 进程                        WebView
    │                              │                              │
    ├─ spawn(token) ──────────────▶│                              │
    │                              ├─ 打开 config dir / YAML       │
    │                              ├─ listen 127.0.0.1:0           │
    │◀── stdout: {"type":"ready",  │                              │
    │      "port":52341,...} ──────┤                              │
    ├─ 存入 BackendState           │                              │
    │                              │◀──── ws://127.0.0.1:52341/ws?token=…
    │                              │──── 101 Switching Protocols ─▶│
    │                              │◀──── app.info / config.get … ─┤
    │                              │──── responses ───────────────▶│
```

- **启动不等待 SSH。** 前端分两段启动：第一段只做握手 + 读 YAML + 列本地目录（毫秒级），完成后立刻撤掉启动页；第二段在后台重连上次的远程会话，进度显示在那个面板里，可随时取消。远程会话若没有已保存的凭据则不会自动连接，避免开机弹密码框。
- 后端另有两道兜底：`session.connect` 的上下文被裁剪为 `connectTimeoutSeconds + 5s`；SSH 版本握手的 socket deadline 取「配置超时」与「调用方 deadline」的较小值。否则一个「端口通但不说话」的主机会把请求拖到 IPC 的 5 分钟上限。
- 端口由内核分配（`127.0.0.1:0`），避免固定端口冲突。
- token 由 Rust 用 `getrandom` 生成 24 字节并注入，Go 端常量时间比较；`Origin` 仅允许 `tauri://`、`localhost`、`127.0.0.1`。
- 进程只监听回环地址，不对外网暴露。

### 3.2 报文格式

请求 / 响应 / 事件三种帧：

```json
{"id":"17","type":"request","method":"fs.list","params":{"side":"local","path":"~","showHidden":false}}
{"id":"17","type":"response","ok":true,"result":{"path":"/Users/me","entries":[…]}}
{"id":"17","type":"response","ok":false,"error":{"code":"AUTH_FAILED","message":"认证失败","detail":"ssh: unable to authenticate"}}
{"type":"event","event":"transfer.task","payload":{"task":{…}}}
```

- `id` 由前端自增，后端原样回传；前端按 id 关联 Promise。
- 事件无 id，按 `event` 名分发；目前只有 `transfer.task`（任务任意状态变化都会推送完整任务快照，前端 upsert 即可）。
- 进度事件按 150ms 节流，避免大文件传输时刷爆 WebSocket。

### 3.3 错误码

| code | 含义 | 前端行为 |
| --- | --- | --- |
| `BAD_REQUEST` | 参数非法 | toast |
| `NOT_FOUND` | 路径不存在 | 面板内错误态 + 重试 |
| `PERMISSION_DENIED` | 无权限 | toast |
| `ALREADY_EXISTS` | 重名 | toast |
| `UNSUPPORTED` | 平台/能力不支持 | toast |
| `AUTH_FAILED` | SSH 认证失败 | 弹出密码对话框 |
| `HOST_KEY_MISMATCH` | 主机密钥变化 | 阻断并提示风险 |
| `CONNECT_FAILED` | 网络/端口/DNS | toast |
| `NOT_CONNECTED` | 未连接远程 | toast / 引导连接 |
| `CANCELED` | 用户取消 | 静默 |
| `INTERNAL` | 兜底 | toast（含 detail） |

### 3.4 RPC 方法表

| 分组 | 方法 |
| --- | --- |
| 应用 | `app.info` `app.shutdown` `config.get` `config.set` `config.reveal` |
| 会话 | `session.list` `session.save` `session.delete` `session.connect` `session.disconnect` `session.test` `session.status` |
| 文件 | `fs.list` `fs.stat` `fs.home` `fs.places` `fs.mkdir` `fs.rename` `fs.delete` `fs.open` `fs.reveal` |
| 收藏 | `favorite.list` `favorite.add` `favorite.remove` |
| 传输 | `transfer.create` `transfer.list` `transfer.cancel` `transfer.retry` `transfer.remove` `transfer.clear` |
| 界面 | `ui.state.get` `ui.state.set` |
| 信任 | `known_hosts.list` `known_hosts.delete` |

---

## 4. YAML 持久化

### 4.1 目录

```
~/Library/Application Support/file-j/     # 或 --config-dir 指定
├── config.yaml          # 外观、传输默认值、SSH 策略、日志级别
├── sessions.yaml        # 会话定义（绝不含密码）
├── ui-state.yaml        # 双栏路径、会话、排序、分隔比例、活动面板
├── favorites.yaml       # 收藏的本地文件夹（侧边栏「收藏」区）
├── known_hosts.yaml     # TOFU 主机密钥指纹
└── logs/backend.log     # 文本日志，>4MB 启动时轮换
```

五个文档分离的理由：手改坏一个不会拖垮其他；`ui-state.yaml` 写入频繁（每次导航/排序），不应反复重写会话或收藏配置。

### 4.2 `config.yaml`

```yaml
version: 1
ui:
  theme: system            # system | light | dark
  density: comfortable     # comfortable | compact
  showHiddenFiles: false
  sidebarVisible: true
  transferPanelOpen: true
  confirmDelete: true
  dateFormat: localized    # localized | iso
transfer:
  concurrency: 3           # 1..8
  conflict: overwrite      # overwrite | skip | keepboth | fail
  preserveTimes: true
  preservePermissions: true
  bufferSizeKB: 128
remote:
  connectTimeoutSeconds: 15
  keepAliveSeconds: 30
  hostKeyPolicy: tofu      # strict | tofu | insecure
  maxPacketSize: 32768
  idleDisconnectMinutes: 15
```

### 4.3 `sessions.yaml`

```yaml
version: 1
sessions:
  - id: demo01
    name: 演示服务器
    protocol: sftp
    host: 127.0.0.1
    port: 2222
    user: demo
    authMethod: password        # password | key | agent
    keyPath: ~/.ssh/id_ed25519
    credentialRef: session/demo01   # Keychain 条目名，不是密码本身
    remotePath: /Users/Shared/filej-remote
    color: purple
    lastUsedAt: 2026-10-19T22:00:00Z
```

**密码不落盘。** `password` 只在 `session.save` 请求中出现一次，写入系统凭据存储（macOS 钥匙串 `security add-generic-password -s com.jiangwanyi.filej`；Windows 凭据管理器 `CredWriteW`，目标名 `FileJ/<account>`），YAML 仅保存 `credentialRef`。凭据存储不可用时退化为“仅本次运行有效”，并在 UI 明确提示。

### 4.4 `ui-state.yaml`

```yaml
version: 1
left:  { kind: local,  path: /Users/me/project, sortKey: name, sortDesc: false }
right: { kind: remote, path: /srv/www, sessionId: demo01, sortKey: name, sortDesc: false }
splitRatio: 0.5
activePane: left
```

### 4.5 `favorites.yaml`

```yaml
version: 1
items:
  - path: /Users/me/project
    name: project
    addedAt: 2026-10-22T21:00:00Z
```

只收录**本地文件夹**：SFTP 路径离开会话就没有意义，侧边栏已有会话列表。`favorite.add` 在 Go 侧展开 `~`/相对路径并 `stat` 校验，非目录或不存在直接拒绝（`NOT_FOUND` / `BAD_REQUEST`）；同一路径重复收藏为幂等操作，保持插入顺序。加载时丢弃空路径与重复项，手改坏文件只会少几条收藏。

### 4.6 `known_hosts.yaml`

```yaml
version: 1
hosts:
  - host: 127.0.0.1:2222
    algorithm: ssh-ed25519
    fingerprint: SHA256:…
    addedAt: 2026-10-19T22:00:00Z
```

### 4.7 写入策略

- 每次写：`写临时文件 → fsync → rename`，避免半截文件；若目标卷或安全软件拒绝 rename（FAT/exFAT、部分终端防护），退化为原地重写，保证配置仍能落盘。
- 权限 `0600`，目录 `0700`。
- 读取时 `Normalize()` 收敛所有枚举与数值范围；手改成非法值只回退到默认，不会崩溃。
- 单一写入者（只有 Go 进程写），因此不需要文件锁。
- 传输队列、进度、日志不持久化：重启后队列清空是有意设计（重放未完成的写操作风险更高）。

---

## 5. 功能实现边界

### 5.1 双栏浏览与管理

| 能力 | 实现方式 | 边界 |
| --- | --- | --- |
| 左右面板任意为本地/远程 | `Pane.side` + `sessionId` | 远程到远程同屏不提供（见 5.5） |
| 路径导航 | 后端返回绝对 `path`/`parent`；前端维护 history | 输入框支持 `~`、相对路径（本地） |
| 排序 | 前端按 名称/大小/时间/权限 排序，目录恒在前 | 不记忆每个目录的独立排序，只有每面板一份 |
| 隐藏文件 | 由配置文件开关控制，后端过滤 | 不区分“系统隐藏”与“点文件” |
| 多选 | 单击 / ⌘ / ⇧ / ⌘A | 不支持框选（rubber band） |
| 新建 / 重命名 / 删除 | `fs.mkdir` / `fs.rename` / `fs.delete` | 重命名只改同目录内名称，不支持跨目录移动式重命名 |
| 删除语义 | 本地移入 `~/.Trash`（跨卷时退化为永久删除）；远程永久删除 | UI 明确提示差异；不做撤销 |
| 打开文件 | 本地 `open(1)`；远程文件双击 = 下载到对侧 | 不内建预览器 / 编辑器 |
| 拖拽 | 面板间 HTML5 DnD，落在文件夹行可指定目标目录 | 不做面板内拖拽移动（用 ⌘X/右键移动） |
| 快捷键 | F5 复制、F6 移动、⌫ 上级、⌘⌫ 删除、⌘R 刷新、⇧⌘N 新建 | 与 WinSCP 习惯对齐 |
| 收藏夹 | 右键本地文件夹（或空白处收藏当前文件夹）→ `favorite.*` 写入 `favorites.yaml`；侧边栏点击即跳转 | 只支持本地目录；远程路径依赖会话，不进收藏 |

### 5.2 本地 ↔ 本地

- **复制**：递归遍历，逐文件流式拷贝（默认 128KB 缓冲），保留 mtime/权限（可关）。
- **移动**：同一 `Provider` 先尝试 `os.Rename`（瞬间完成、不产生流量）；跨卷失败后自动退化为“复制 + 删除源”。
- **符号链接**：默认不跟随（避免自引用导致无限递归），直接跳过并在任务中体现；`followSymlinks` 目前仅作为配置预留，未启用跟随。
- **冲突**：策略统一由 `transfer.conflict` 决定，见 5.4。

### 5.3 SSH / SFTP 远程

| 能力 | 实现 |
| --- | --- |
| 认证 | 密码、私钥（OpenSSH/PEM，可带口令）、ssh-agent；密码方式同时注册 `keyboard-interactive` 以兼容只在交互式下认证的服务器 |
| 连接复用 | 每个会话一条 `ssh.Client` + `sftp.Client`，目录切换不再重连 |
| 保活 | 可配置 `keepalive@openssh.com` 间隔 |
| 空闲回收 | 后台每 30s 检查，超过 `idleDisconnectMinutes` 主动断开 |
| 主机密钥 | `strict`（必须已存在且匹配）/ `tofu`（首次记录）/ `insecure`（不校验）；变更时返回 `HOST_KEY_MISMATCH` 并阻断 |
| 列表 | `ReadDir` + `Lstat`/`Stat` 处理软链接，展示 uid/gid 与 `ls -l` 权限串 |
| 磁盘容量 | `StatVFS`，服务器不支持时隐藏该字段 |
| 断线 | 传输任务报错并保留在队列，可重试；UI 状态栏提示 |

### 5.4 传输引擎

```
transfer.create(items, op, conflict)
        │
        ▼
   队列(queued) ──► 调度器（并发上限来自 config.transfer.concurrency）
        │
        ▼
  解析两侧 Provider（必要时建连）
        │
        ├─ 移动且同一 Provider → Rename 快路径（0 字节）
        ├─ 目标根冲突处理（overwrite/skip/keepboth/fail）
        ├─ 预扫描总字节/文件数（上限 20 万项，超出则进度条改为不确定态）
        ├─ 递归复制（每文件检查 ctx 取消）
        └─ 完成后 move 删除源
        │
        ▼
  transfer.task 事件（150ms 节流）→ 前端 upsert
```

- **进度**：字节数、文件数、瞬时速度；总大小未知时显示不确定进度条。
- **取消**：每个任务独立 `context`，读取循环中检查，可中断大文件中途。
- **重试**：以原参数重新入队（新 ID）。
- **并发**：默认 3；同一 SSH 连接上多任务并发（`pkg/sftp` 支持并发请求）。
- **历史**：内存中最多保留 200 条，重启清空。
- **冲突策略**：`overwrite` 覆盖、`skip` 跳过、`keepboth` 自动追加 ` (1)`、`fail` 中止任务。策略在创建任务时固定，运行中改设置不影响已排队任务。

### 5.5 明确不实现的能力

| 不做 | 原因 / 替代 |
| --- | --- |
| 远程 → 远程直接传输 | 需要服务端中转或第三方中继；当前会直接拒绝并提示 |
| 断点续传 / 分片校验 | 需要持久化任务状态与远端临时文件协议；当前失败即重试整文件 |
| 目录同步 / 镜像 | 属于同步工具范畴，超出文件管理边界 |
| 内置编辑器与保存回传 | 需文件监听与冲突检测；双击远程文件先下载 |
| ProxyJump / 堡垒机 / 端口转发 | 未实现跳板链；可先用系统 `ssh -L` 或 ssh config |
| 修改远程权限/属主 | 仅展示；`Chmod` 已在 Provider 内保留接口但未开放 UI |
| 符号链接跟随复制 | 防止递归环；跳过处理 |
| 队列持久化 | 重启后不重放未完成的写操作 |
| 多窗口 / 多标签 | 单窗口双栏是既定交互模型 |

---

## 6. 平台适配

平台差异集中在三层，**业务代码里没有一处 `runtime.GOOS` 分支**：

| 层 | 机制 | 位置 |
| --- | --- | --- |
| Go | `//go:build windows` / `!windows` 成对文件 | `internal/fsx/{paths,os,trash}_*.go`、`internal/credentials/secret_*.go`、`internal/remote/agent_*.go`、`internal/app/shell_*.go` |
| Rust | `#[cfg(target_os = ...)]` + Tauri 平台配置 | `src-tauri/src/lib.rs`、`src-tauri/Cargo.toml`、`tauri.windows.conf.json` |
| 前端 | 单一模块导出常量 | `src/lib/platform.ts`（键位标签、系统名、是否自绘窗口按钮） |

### 6.1 路径：两种方言共存

这是最容易出错的一处。后端同时处理**本地路径**（跟随宿主系统）与 **SFTP 路径**（永远是 POSIX），而传输引擎对两者调用同一组助手函数。

- `paths_unix.go`：macOS 上本地与远程都是 `/`，一份 `path.Clean/Join/Parent/Base` 即可，行为与改造前完全一致。
- `paths_windows.go`：**按输入本身判断方言**——带盘符或 `\\` 前缀即 Windows 风格，其余按 POSIX 处理。于是 `C:/Users` 用 `filepath` 语义、`/srv/www` 保持 POSIX 语义，两者不会在同一次调用里混用。
- 前端 `format.ts` 的 `basename/dirname/joinPath/isInside` 采用同一套判定（Windows 下比较忽略大小写）。
- 「回到根目录」不硬编码 `/`：新增 `fs.root` RPC，macOS 与 SFTP 返回 `/`，Windows 本地返回卷根 `C://`。

### 6.2 系统能力对照

| 能力 | macOS | Windows 11 |
| --- | --- | --- |
| 打开文件 | `open <path>` | `ShellExecuteW` verb=open |
| 在文件管理器中显示 | `open -R <path>` | `explorer /select,<path>` |
| 删除到废纸篓 | 移入 `~/.Trash`，重名自动加序号 | `SHFileOperationW` + `FOF_ALLOWUNDO`（等同资源管理器的删除） |
| 磁盘容量 | `unix.Statfs` | `GetDiskFreeSpaceExW` |
| 隐藏文件 | `.` 开头 | `.` 开头 **或** `FILE_ATTRIBUTE_HIDDEN` |
| 只读标记 | 由权限串体现 | 显式读 `FILE_ATTRIBUTE_READONLY` |
| 文件名合法性 | 仅禁 `/` 与 NUL | 另禁 `\ < > : " | ? *`、控制字符、结尾空格/点、CON/PRN/AUX/NUL/COM1-9/LPT1-9 |
| 密钥存储 | Keychain（`security` CLI） | 凭据管理器（`CredReadW`/`CredWriteW`/`CredFree`） |
| ssh-agent | `SSH_AUTH_SOCK` unix socket | `SSH_AUTH_SOCK` 命名管道（OpenSSH for Windows） |
| 侧边栏根位置 | 单个「根目录」`/` | 枚举已挂载的盘符 |
| 配置目录 | `os.UserConfigDir()` → `~/Library/Application Support/file-j` | 同函数 → `%APPDATA%\file-j` |

跨卷移动在两个平台都要回退到「复制 + 删除」：macOS 判 `unix.EXDEV`，Windows 判 `ERROR_NOT_SAME_DEVICE`。

### 6.3 窗口外壳

| | macOS | Windows 11 |
| --- | --- | --- |
| 窗口配置 | `tauri.conf.json`：`titleBarStyle: Overlay` | `tauri.windows.conf.json`：`decorations: false` + `shadow: true`（保留投影与 Win11 圆角） |
| 窗口按钮 | AppKit 原生红绿灯，由 `center_traffic_lights`（`#[cfg(target_os="macos")]`）垂直居中到 52px 中线 | 前端 `Toolbar.vue` 自绘最小化/最大化/关闭，SVG 图元 |
| 顶栏左侧留白 | `pl-[80px]` 给红绿灯 | `pl-3`，不留白 |
| 双击标题栏 | 系统行为（缩放） | 显式 `toggleMaximize()` |
| 打包目标 | `app` + `dmg`，`minimumSystemVersion 12.0` | `nsis` + `msi` |

顶栏高度 52px 是跨平台契约：macOS 侧是 `Toolbar.vue` 的 `h-[52px]` ↔ `lib.rs` 的 `TITLE_BAR_HEIGHT`，Windows 侧靠 `items-center` 让自绘按钮落在同一条中线上。

`titleBarStyle` 在 Tauri 里是 `#[cfg(target_os = "macos")]` 的字段，非 macOS 平台被完全忽略，所以 Windows 配置里不写它。无边框窗口的缩放由 tao 通过 `WM_NCHITTEST` 逐边处理，边缘 4px 内拖动仍可调整大小。

窗口按钮走 Tauri IPC，因此 `capabilities/default.json` 需要 `core:window:allow-minimize` / `allow-toggle-maximize` / `allow-is-maximized` / `allow-close`；`allow-start-dragging` 与 `allow-internal-toggle-maximize` 是拖拽区（含双击最大化）所需。

sidecar 以 `file-jd.exe`（Windows）/ `file-jd`（macOS）命名并由 `std::env::consts::EXE_SUFFIX` 拼出；Windows 上 spawn 时带 `CREATE_NO_WINDOW`，避免每次启动闪一个控制台窗口。

### 6.4 依赖与工具链

| 决策 | 原因 |
| --- | --- |
| **Tailwind CSS 3.4 而非 4.x** | v4 依赖 `@property` 与 `color-mix()`，需 Safari 16.4+，在 macOS 12 上样式会整体失效 |
| Vite `build.target = safari15` | 默认目标会输出 Safari 15 不支持的语法 |
| `bundle.macOS.minimumSystemVersion = "12.0"` | 明确产物最低系统版本 |
| 不用 `structuredClone`、`:has()` 等新 API | 避免运行时不可用；已改用 JSON 深拷贝与类名控制 |
| 前端不依赖 Node/系统命令 | 所有系统能力都经 Go 后端 |
| Go 静态编译（`CGO_ENABLED=0`） | 无外部动态库依赖，Intel/Apple Silicon 与 Windows 均可构建 |
| `scripts/build-backend.mjs` 而非 `.sh` | Windows 上不保证有 bash；`pnpm backend:build` 两个平台同一条命令 |
| `.gitattributes` 统一 LF | 避免在 Windows 上 checkout 后整文件显示为已修改；仅 `*.bat/*.cmd/*.ps1` 例外用 CRLF |
| 前端 `vite build` / `vue-tsc` 只做类型与产物校验 | 不依赖 shell，Windows 上可直接执行 |


---

## 7. 构建与运行

```bash
# 依赖
pnpm install
go version          # 1.26.9

# 1) 仅前端调试（浏览器 + 独立后端）
pnpm backend:run    # 127.0.0.1:47121, token=dev, 配置写入 ./.devconfig
pnpm dev            # http://localhost:1420

# 2) 完整桌面应用
pnpm tauri dev      # 自动构建 sidecar 并启动窗口

# 3) 打包（macOS: .app / .dmg；Windows: NSIS / .msi）
pnpm tauri build

# 4) 本地演示用 SFTP 服务器（可选）
cd backend && go run ./cmd/demo-sshd --addr 127.0.0.1:2222 --user demo --password demo

# 5) 测试
pnpm backend:test   # Go 单元 + 集成测试（含内置 SSH/SFTP 端到端）
pnpm build          # vue-tsc + vite build
```

`pnpm backend:build`（等价 `node scripts/build-backend.mjs`）按 `rustc` 的 host triple 生成
`src-tauri/binaries/file-jd-<triple>`，Windows 上带 `.exe` 后缀 —— 这是 Tauri `externalBin` 要求的命名。
脚本内用 `rustc -vV` 取 triple，取不到时退回 `go env GOOS/GOARCH`，因此没有 Rust 也能先验证 Go 侧构建。

---

## 8. 测试策略

| 层 | 测试 |
| --- | --- |
| `config` | 默认值落盘、往返读写、越界收敛、会话增删改、UI 状态、known_hosts |
| `remote` | 进程内真实 SSH+SFTP 服务器：认证、浏览、读写、重命名、递归删除、TOFU 记录、密钥变更阻断、错误密码 |
| `transfer` | 本地复制/递归/移动、四种冲突策略、SFTP 上传下载、递归下载、排队取消、远程到远程拒绝 |
| 前端 | `vue-tsc` 严格类型检查；构建产物按 Safari 15 目标编译 |
| `fsx` | 路径方言判定、Join/Parent/Base 的双语义、`RootPath` 落在真实目录、平台文件名规则 |
| 界面 | 通过 Chrome DevTools Protocol 真实点击、按 F5、切换主题并截图验证 |

测试用 SSH 服务器在进程内启动（`internal/testutil`），因此 CI 无需外部依赖，也不触碰真实主机。

---

## 9. 目录结构

```
backend/
├── cmd/file-jd/            # 生产入口（握手、日志、信号）
├── cmd/demo-sshd/          # 开发用 SFTP 服务器
└── internal/
    ├── app/                # RPC 处理器与装配
    ├── config/             # YAML 模型与存储
    ├── credentials/        # Keychain / 凭据管理器
    ├── fsx/                # Provider 抽象 + 本地实现（含平台适配文件）
    ├── ipc/                # WebSocket 服务
    ├── protocol/           # 报文与错误码
    ├── remote/             # SSH/SFTP
    ├── transfer/           # 传输引擎
    └── testutil/           # 测试服务器
src/
├── lib/                    # 协议类型、WS 客户端、格式化、菜单类型
│   ├── platform.ts         # 平台常量：键位标签 / 系统名 / 是否自绘窗口按钮
│   └── window.ts           # 窗口控制（仅 Windows 需要）
├── stores/workspace.ts     # 单一响应式状态 + 全部动作
└── components/             # Toolbar / Sidebar / Pane / FileTable / TransferPanel / 对话框
src-tauri/                  # Tauri 壳层（仅框架职责）
scripts/
├── build-backend.mjs        # sidecar 构建（跨平台，pnpm backend:build 实际执行）
└── build-backend.sh         # 同上，保留给习惯直接调用的场景
```
