# hunyuan3d-proxy — 工程约定

本项目是**纯 Go** 项目：运行、构建、测试只需要 Go 工具链。
唯一的前端是浏览器里执行的 `web/ui/*.js`（由 Go 直接以静态文件提供），
**不要引入 `package.json` / `node_modules` / Node / Bun / Playwright 等 JS 工具链。**

## 命令

- 运行：`go run .`（环境变量 `PORT` / `DATA_DIR` / `UPSTREAM`），或 `./scripts/start.sh`
- 构建：`go build -o hunyuan3d-proxy .`；**构建产物不提交仓库**（.gitignore 已覆盖 .exe / dist/）
- 测试：`go test ./...`（离线可跑，不访问上游）
- 格式化：`gofmt -w .`
- 发布：打 tag 即触发 `.github/workflows/release.yml`（矩阵构建 win/linux/macOS × amd64/arm64 → 自动发 Release）：
  `git tag v0.1.0 && git push origin v0.1.0`
- MCP：随 HTTP 服务自动启用，端点 `POST /mcp`（Streamable HTTP，见 `docs/mcp.md`）

## 约定

- 本地路由集中在 `routes.go` 的 `buildMux`；Web UI 静态文件由 Go 从 `web/` 直接读取（无需打包/嵌入）。
- API 响应统一使用 `respond.go` 的 `writeOK` / `writeErr` 信封 `{ok,data,error}`。
- 敏感字段（手机号、token、innerUserId 等）必须经 `helpers.go` 的 `scrubSensitive` / `maskID` 脱敏后再返回。
- 登录为纯协议实现（`login.go`、`login_qr.go`、`wxlogin.go`），不得依赖浏览器自动化。
- 新增路由时同步更新 `web/openapi.json`，并在 `ui_smoke_test.go` 增加断言。
- 上游请求的签名逻辑见 `signature.go` / `docs/signature.md`。
- 3D 生成是**两阶段**：几何（白模，`materials=0`）→ 纹理（`image2texture_v31`）。
  只拿到白模不算完成；验收要看 GLB 的 `materials > 0`。
  文生纹理用 `texture_edit_local` + `editType:1`（**不是** `text2texture`，那个上游有缺陷）；
  且 fbxUrl 必须已 UV 展开，否则跑完无产物。详见 `docs/proxy-api.md`。
- **MCP 不暴露账号能力**：不得新增 导入Cookie/登出/积分/登录态 类工具，
  `hunyuan_health` 也不得返回 cookieCount/sessionLike。登录与额度只在 Web 控制台操作。
- MCP 层：`mcp.go`（协议核心，传输无关）、`mcp_tools.go`（工具目录与输出裁剪）、
  `mcp_http.go`（HTTP 传输 + 面板 API）。**只有 HTTP 一种传输，没有 stdio**；
  新增能力时同时补 `mcp_test.go` / `mcp_http_test.go` 断言。
- 安全默认值：HTTP 服务默认绑定 `127.0.0.1`；`/mcp` 默认不校验令牌，但**必须**保留跨源拒绝，
  且任何"绑定到非 loopback"的路径都要提醒用户设置 `MCP_HTTP_TOKEN`。
