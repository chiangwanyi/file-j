#!/usr/bin/env bash
# Builds the Go business backend as a Tauri sidecar.
#
# Kept for anyone calling this directly (and for muscle memory); the real
# implementation is scripts/build-backend.mjs so that the same command works
# under cmd.exe/PowerShell on Windows, where bash is not guaranteed to exist.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
exec node "$ROOT/scripts/build-backend.mjs"