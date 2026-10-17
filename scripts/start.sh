#!/usr/bin/env sh
# Start hunyuan3d-proxy (Go only — no Node/Bun required).
set -e
root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"

export PORT="${PORT:-8812}"
export DATA_DIR="${DATA_DIR:-$root/data}"
export UPSTREAM="${UPSTREAM:-https://3d.hunyuan.tencent.com}"
mkdir -p "$DATA_DIR"

echo "UI: http://127.0.0.1:$PORT/"
exec go run .
