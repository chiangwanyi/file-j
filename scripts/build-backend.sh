#!/usr/bin/env bash
# Builds the Go business backend as a Tauri sidecar.
#
# Tauri requires the binary to be named <name>-<target-triple> inside
# src-tauri/binaries so it can bundle the right architecture.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

TRIPLE="$(rustc -vV | awk '/^host: /{print $2}')"
if [[ -z "${TRIPLE}" ]]; then
  echo "无法从 rustc 获取目标平台三元组" >&2
  exit 1
fi

VERSION="$(node -p "require('./package.json').version" 2>/dev/null || echo "0.1.0")"
OUT_DIR="src-tauri/binaries"
OUT="${OUT_DIR}/file-jd-${TRIPLE}"

mkdir -p "$OUT_DIR"
echo "==> building backend ${VERSION} for ${TRIPLE}"
cd backend
CGO_ENABLED=0 go build \
  -trimpath \
  -ldflags "-s -w -X main.version=${VERSION}" \
  -o "../${OUT}" \
  ./cmd/file-jd
cd "$ROOT"

chmod +x "$OUT"
echo "==> ${OUT} ($(du -h "$OUT" | awk '{print $1}'))"
