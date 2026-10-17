#!/usr/bin/env sh
# Build (if stale) and start hunyuan3d-proxy. Go only — no Node/Bun required.
set -e
root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"

export PORT="${PORT:-8812}"
export DATA_DIR="${DATA_DIR:-$root/data}"
export UPSTREAM="${UPSTREAM:-https://3d.hunyuan.tencent.com}"
mkdir -p "$DATA_DIR"

if [ ! -x ./hunyuan3d-proxy-linux ] || [ -n "$(find . -maxdepth 1 -name '*.go' -newer ./hunyuan3d-proxy-linux)" ]; then
  echo "building hunyuan3d-proxy-linux ..."
  go build -o hunyuan3d-proxy-linux .
fi

echo "UI: http://127.0.0.1:$PORT/"
exec ./hunyuan3d-proxy-linux
