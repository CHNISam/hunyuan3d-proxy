# hunyuan3d-proxy — 单文件夹布局

> ⚠️ **本项目仅用于学习用途**，请遵守腾讯服务条款，勿用于商业用途。
项目已在linux.do公开声明https://linux.do/
本目录是 **腾讯混元3D 反代服务器** 的完整集合（源码 / UI / 文档 / 脚本 / 数据）。
**纯 Go 项目**：运行、测试只需 Go 工具链，不需要 Node / Bun / Playwright。
（`web/ui/*.js` 是浏览器里跑的控制台前端，不参与构建。）

```
hunyuan3d-proxy/
├── main.go, proxy.go, routes.go, login*.go, credits.go, text3d.go,
│   assets.go, cloud.go, signature.go, jar.go, config.go, ...
├── ui_smoke_test.go         # Web UI / HTTP 冒烟测试（go test）
├── mcp.go, mcp_tools.go     # 内置 MCP server（HTTP，纯标准库）
├── mcp_http.go, mcp_test.go, mcp_http_test.go
├── go.mod
├── hunyuan3d-proxy.exe      # Windows 主程序（与 bin/ 中最新构建一致）
├── hunyuan3d-proxy-linux    # Linux 构建产物（go build 生成，已 gitignore）
├── bin/                     # 历史/最新 exe
├── web/
│   ├── index.html           # Web 控制台入口
│   ├── docs.html, openapi.json
│   └── ui/app.css, app.js, qr.js
├── docs/
│   ├── mcp.md               # MCP 接入文档
│   ├── proxy-api.md         # 本地 API 文档
│   ├── signature.md
│   ├── api-catalog.md
│   └── ...
├── scripts/
│   ├── start.bat / start.ps1  # Windows 启动
│   └── start.sh               # Linux 启动（自动构建）
├── data/
│   └── cookies.json         # 登录会话（勿提交仓库）
└── README.md
```

## 快速启动

Linux / macOS：

```sh
./scripts/start.sh          # 自动 go build 后启动，默认 8812
# 或手动：
PORT=8812 DATA_DIR="$PWD/data" UPSTREAM="https://3d.hunyuan.tencent.com" \
  ./hunyuan3d-proxy-linux
```

Windows PowerShell：

```powershell
cd D:\lose\hunyuan3d-proxy

$env:PORT = "8812"
$env:DATA_DIR = "D:\lose\hunyuan3d-proxy\data"
$env:UPSTREAM = "https://3d.hunyuan.tencent.com"
.\hunyuan3d-proxy.exe
```

或：`.\scripts\start.ps1`

浏览器打开：`http://127.0.0.1:8812/`

API 文档（Swagger UI）：`http://127.0.0.1:8812/docs` · 规范：`/openapi.json`

## 编译

```sh
go build -o hunyuan3d-proxy-linux .        # 当前平台
```

Windows：

```powershell
$env:GOROOT = "D:\lose\.tools\go"
& "D:\lose\.tools\go\bin\go.exe" build -o hunyuan3d-proxy.exe .
Copy-Item hunyuan3d-proxy.exe bin\ -Force
```

## 测试（仅需 Go）

```sh
go test ./...        # 全部
go test -v ./...     # 逐条
```

`ui_smoke_test.go` 用 `httptest` 覆盖原先 Playwright 脚本的断言：控制台 HTML 关键元素、
静态资源 `/ui/*` 与 `/docs`、OpenAPI 契约、登录字段脱敏、模式切换、CORS 预检、404 信封、
静态资源 `Cache-Control: no-store`。另有 `signature_test.go`、`helpers_scrub_test.go`
覆盖签名与脱敏逻辑。全部离线可跑，不访问上游。

## MCP（给 LLM 客户端用）

内置 MCP server（纯标准库实现），**随 HTTP 服务自动启用**，端点为 `POST /mcp`（Streamable HTTP）：

```sh
# 无需额外启动：MCP 就在 8812 上
curl -s http://127.0.0.1:8812/mcp -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

共 8 个工具：健康检查、模式切换、资源看板、概念站/主站文生3D、纹理贴图。**不暴露账号能力**（登录/积分只在 Web 控制台）。
**默认不校验令牌**，但 `/mcp` 能无确认消耗积分，所以服务**默认只绑定 127.0.0.1**；
若要 `HOST=0.0.0.0` 或做端口转发，请同时设置 `MCP_HTTP_TOKEN` 开启鉴权。

Web 控制台的 **MCP 服务**面板展示：状态、端点、工具目录与入参 schema、调用示例一键复制；
未启用鉴权时会显示黄色风险提示。

详见 `docs/mcp.md`。

## 主要能力

| 能力 | 说明 |
|------|------|
| 双端点 | main `/api/3d/*` · concept `/game3d/*`，**积分独立、能力不同**；选法见 `docs/proxy-api.md` |
| 扫码登录 | **微信纯协议直登**（qrconnect 轮询 + `POST /api/login/joint` 换会话入库）；**QQ 扫码已废弃** → Cookie 导入 |
| 文生3D | `/api/text3d/main`（主站）· `/api/text3d/generate`（concept） |
| 纹理贴图 | `POST /api/text3d/texture`（MCP: `hunyuan_texture`）：几何白模 → 带 PBR 的成品 GLB |
| 资源看板 | `/api/text3d/assets` + Web UI Toast/历史 |
| 云 API | `/api/cloud/*` 对接腾讯云 ai3d（需 SecretId/Key） |
| API 文档 | `/docs`（Swagger UI）+ `/openapi.json`（OpenAPI 3.0） |

完整接口见 `docs/proxy-api.md`。

## 免责

**本项目仅用于学习用途**，请遵守腾讯服务条款与相关法律法规；使用本项目产生的一切后果由使用者自行承担。
