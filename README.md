# hunyuan3d-proxy — 单文件夹布局

> ⚠️ **本项目仅用于学习用途**，请遵守腾讯服务条款，勿用于商业用途。

本目录是 **腾讯混元3D 反代服务器** 的完整集合（源码 / UI / 文档 / 脚本 / 数据）。

```
hunyuan3d-proxy/
├── main.go, proxy.go, routes.go, login*.go, credits.go, text3d.go,
│   assets.go, cloud.go, signature.go, jar.go, config.go, ...
├── go.mod
├── hunyuan3d-proxy.exe      # 主程序（与 bin/ 中最新构建一致）
├── bin/                     # 历史/最新 exe
├── web/
│   ├── index.html           # Web 控制台入口
│   └── ui/app.css, app.js, qr.js
├── docs/
│   ├── proxy-api.md         # 本地 API 文档
│   ├── signature.md
│   ├── api-catalog.md
│   └── ...
├── scripts/
│   ├── hy_login_worker.js   # Playwright 扫码 worker
│   └── test_*.js            # 冒烟/测试脚本
├── data/
│   └── cookies.json         # 登录会话（勿提交仓库）
└── README.md
```

## 快速启动（Windows PowerShell）

```powershell
cd D:\lose\hunyuan3d-proxy

# 可选：扫码 worker（真实微信二维码）
$env:NODE_PATH = "D:\lose\.tools\pw\node_modules"
Start-Process -FilePath "D:\lose\.tools\node\node.exe" `
  -ArgumentList "scripts\hy_login_worker.js" -WindowStyle Hidden
# 默认 LOGIN_WORKER=http://127.0.0.1:8793

$env:PORT = "8812"
$env:DATA_DIR = "D:\lose\hunyuan3d-proxy\data"
$env:UPSTREAM = "https://3d.hunyuan.tencent.com"
$env:LOGIN_WORKER = "http://127.0.0.1:8793"
.\hunyuan3d-proxy.exe
```

或：

```powershell
.\scripts\start.ps1
```

浏览器打开：`http://127.0.0.1:8812/`

## 编译

```powershell
$env:GOROOT = "D:\lose\.tools\go"
& "D:\lose\.tools\go\bin\go.exe" build -o hunyuan3d-proxy.exe .
Copy-Item hunyuan3d-proxy.exe bin\ -Force
Copy-Item web\* . -Force   # 无需；服务直接读 web/
```

## 主要能力

| 能力 | 说明 |
|------|------|
| 双端点 | main `/api/3d/*` · concept `/game3d/*`，积分独立 |
| 扫码登录 | 微信/QQ + Cookie 导入；redirect 用官网 `/scan?nonce` |
| 文生3D | `/api/text3d/main`（主站）· `/api/text3d/generate`（concept） |
| 资源看板 | `/api/text3d/assets` + Web UI Toast/历史 |
| 云 API | `/api/cloud/*` 对接腾讯云 ai3d（需 SecretId/Key） |

完整接口见 `docs/proxy-api.md`。

## 免责

**本项目仅用于学习用途**，请遵守腾讯服务条款与相关法律法规；使用本项目产生的一切后果由使用者自行承担。
