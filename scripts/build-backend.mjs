// Builds the Go business backend as a Tauri sidecar.
//
// Tauri requires the binary to be named <name>-<target-triple> inside
// src-tauri/binaries so it can bundle the right architecture; on Windows the
// executable suffix is part of that name too, because Tauri appends it when
// resolving `externalBin`.
//
// This is plain Node (no shell) so `pnpm backend:build` behaves identically on
// macOS and Windows. scripts/build-backend.sh is kept as a thin wrapper for
// anyone used to calling it directly, and simply forwards here.

import { execFileSync } from "node:child_process";
import { mkdirSync, readFileSync, statSync, chmodSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..");

/** Runs a command and returns its trimmed stdout, or null when it is missing. */
function capture(command, args) {
  try {
    return execFileSync(command, args, { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }).trim();
  } catch {
    return null;
  }
}

/**
 * Target triple of the host toolchain, matching what Tauri expects.
 *
 * `rustc -vV` is the source of truth because it is the same tool that compiles
 * the shell; falling back to process.arch keeps the script usable in a shell
 * that has Go but no Rust (the Go binary is still perfectly valid, it just
 * would not be picked up by a Tauri build).
 */
function targetTriple() {
  const rustc = capture("rustc", ["-vV"]);
  if (rustc) {
    const line = rustc.split(/\r?\n/).find((l) => l.startsWith("host: "));
    if (line) return line.slice("host: ".length).trim();
  }
  // No Rust on this machine: the Go build still succeeds, it just would not be
  // picked up by a Tauri bundle. Fall back to the Go host's own triple.
  const goEnv = capture("go", ["env", "GOOS", "GOARCH"]);
  if (goEnv) {
    const [goos, goarch] = goEnv.split(/\r?\n/).filter(Boolean);
    return `${goos}_${goarch}`;
  }
  return `${process.platform}_${process.arch}`;
}

const triple = targetTriple();

const pkg = JSON.parse(readFileSync(join(ROOT, "package.json"), "utf8"));
const version = pkg.version ?? "0.1.0";
const exeSuffix = process.platform === "win32" ? ".exe" : "";
const outDir = join(ROOT, "src-tauri", "binaries");
const outFile = join(outDir, `file-jd-${triple}${exeSuffix}`);

mkdirSync(outDir, { recursive: true });
console.log(`==> building backend ${version} for ${triple}`);

// CGO_ENABLED=0 keeps the binary static, matching the previous shell script.
const args = [
  "build",
  "-trimpath",
  "-ldflags",
  `-s -w -X main.version=${version}`,
  "-o",
  outFile,
  "./cmd/file-jd",
];
execFileSync("go", args, {
  cwd: join(ROOT, "backend"),
  stdio: "inherit",
  env: { ...process.env, CGO_ENABLED: "0" },
});

// The execute bit only exists on unix; Windows derives it from the extension.
if (process.platform !== "win32") {
  chmodSync(outFile, 0o755);
}

const size = (statSync(outFile).size / 1024 / 1024).toFixed(1);
const rel = `src-tauri/binaries/file-jd-${triple}${exeSuffix}`;
console.log(`==> ${rel} (${size} MB)`);