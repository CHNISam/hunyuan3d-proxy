@echo off
cd /d "%~dp0\.."
set PORT=8812
set DATA_DIR=%CD%\data
set UPSTREAM=https://3d.hunyuan.tencent.com
if not exist "%DATA_DIR%" mkdir "%DATA_DIR%"
echo UI: http://127.0.0.1:%PORT%/
hunyuan3d-proxy.exe
