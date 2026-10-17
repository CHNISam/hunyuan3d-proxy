# Start hunyuan3d-proxy from this folder (Go only — no Node/Bun needed).
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$proj = Split-Path -Parent $root
Set-Location $proj

$env:PORT = if ($env:PORT) { $env:PORT } else { "8812" }
$env:DATA_DIR = Join-Path $proj "data"
$env:UPSTREAM = if ($env:UPSTREAM) { $env:UPSTREAM } else { "https://3d.hunyuan.tencent.com" }

New-Item -ItemType Directory -Force -Path $env:DATA_DIR | Out-Null

$exe = Join-Path $proj "hunyuan3d-proxy.exe"
if (-not (Test-Path $exe)) {
  $exe = Get-ChildItem (Join-Path $proj "bin\*.exe") | Sort-Object LastWriteTime -Descending | Select-Object -First 1 -ExpandProperty FullName
}

Write-Host "Starting $exe on port $env:PORT"
Write-Host "UI: http://127.0.0.1:$env:PORT/"
& $exe
