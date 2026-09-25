# Start hunyuan3d-proxy from this folder
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$proj = Split-Path -Parent $root
Set-Location $proj

$node = "D:\lose\.tools\node\node.exe"
$env:NODE_PATH = "D:\lose\.tools\pw\node_modules"
$env:PORT = if ($env:PORT) { $env:PORT } else { "8812" }
$env:DATA_DIR = Join-Path $proj "data"
$env:UPSTREAM = if ($env:UPSTREAM) { $env:UPSTREAM } else { "https://3d.hunyuan.tencent.com" }
$env:LOGIN_WORKER = if ($env:LOGIN_WORKER) { $env:LOGIN_WORKER } else { "http://127.0.0.1:8793" }

New-Item -ItemType Directory -Force -Path $env:DATA_DIR | Out-Null

# login worker (optional but recommended for QR)
if ($node -and (Test-Path $node) -and (Test-Path (Join-Path $proj "scripts\hy_login_worker.js"))) {
  Write-Host "Starting login worker on :8793 ..."
  Start-Process -FilePath $node -ArgumentList "scripts\hy_login_worker.js" -WorkingDirectory $proj -WindowStyle Hidden
  Start-Sleep -Seconds 1
}

$exe = Join-Path $proj "hunyuan3d-proxy.exe"
if (-not (Test-Path $exe)) {
  $exe = Get-ChildItem (Join-Path $proj "bin\*.exe") | Sort-Object LastWriteTime -Descending | Select-Object -First 1 -ExpandProperty FullName
}

Write-Host "Starting $exe on port $env:PORT"
Write-Host "UI: http://127.0.0.1:$env:PORT/"
& $exe
