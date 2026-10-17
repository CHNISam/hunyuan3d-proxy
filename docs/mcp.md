# MCP 接入（Model Context Protocol）

`hunyuan3d-proxy` 内置 MCP server（纯 Go 标准库实现，零外部依赖），
让支持 MCP 的客户端把腾讯混元3D 的能力当工具调用。

**只有 HTTP 一种传输**：MCP 端点随 HTTP 服务一起启动，地址为 `POST /mcp`（Streamable HTTP）。
不需要（也不存在）单独的启动方式，服务起来 MCP 就在。

| 项目 | 值 |
|------|-----|
| 端点 | `POST http://127.0.0.1:8812/mcp` |
| 传输 | Streamable HTTP（单个 JSON-RPC 对象一个响应） |
| 协议版本 | `2025-06-18`（同时兼容 `2025-03-26` / `2024-11-05` / `2024-10-07`） |
| 鉴权 | 默认不校验；设 `MCP_HTTP_TOKEN` 后要求 `Authorization: Bearer <token>` |
| 跨源 | 一律拒绝（403） |

### 默认不校验令牌 —— 请先确认暴露面

`/mcp` 能在**不弹任何确认**的情况下消耗真实积分。默认**不校验令牌**（`MCP_HTTP_TOKEN` 未设置），
此时任何能访问该端口的程序都可直接调用工具。

因此**默认绑定被收紧为 `127.0.0.1`**（此前是 `:8812`，即所有网卡）。只有两种情况下才需要开令牌：

- 你显式设置了 `HOST=0.0.0.0` 或其他非 loopback 地址；
- 你对端口做了转发 / 反向代理 / 容器映射。

这两种情况下**务必**同时设置 `MCP_HTTP_TOKEN`：

```sh
HOST=0.0.0.0 MCP_HTTP_TOKEN=$(openssl rand -hex 16) go run .
```

服务启动时若检测到绑定在非 loopback 地址，会在日志里打印告警；未开令牌时告警会明确指出"任何人都能消耗积分"。

无论开不开令牌，**跨源请求一律拒绝（403）**，防的是浏览器页面被第三方站点驱动。

控制台面板会如实显示当前状态：未启用时显示「鉴权：未启用」并给出黄色风险提示，
令牌输入框在未启用时隐藏。

Web 控制台里有 **MCP 服务**面板：连接状态、端点、令牌（默认打码，点击显示）、
工具目录（含每个工具的入参 schema 与"是否消耗积分"标记）、三种接入配置一键复制。

## 启动与调用

MCP 与 HTTP 服务同生共死，无需额外启动：

```sh
PORT=8812 DATA_DIR="$PWD/data" go run .
# MCP 端点即 http://127.0.0.1:8812/mcp
```

默认（未设令牌）直接调用：

```sh
curl -s http://127.0.0.1:8812/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{}}}'
```

开启了令牌则加请求头（值为 `MCP_HTTP_TOKEN` 的内容）：

```sh
curl -s http://127.0.0.1:8812/mcp \
  -H "Authorization: Bearer $MCP_HTTP_TOKEN" -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

`GET /mcp` 是能力探测（返回传输、协议版本、工具数、当前鉴权状态，不消耗积分）；
`GET /api/mcp/panel` 是控制台面板的数据源。通知类消息返回 **202 空响应体**。

## 客户端接入

MCP 客户端配置 HTTP 端点即可（以支持 Streamable HTTP 的客户端为例）：

```json
{
  "mcpServers": {
    "hunyuan3d": {
      "url": "http://127.0.0.1:8812/mcp",
      "headers": { "Authorization": "Bearer ${MCP_HTTP_TOKEN}" }
    }
  }
}
```

未启用令牌时去掉 `headers` 一行。服务端必须处于运行状态（MCP 依附于 HTTP 服务）。

## 工具清单

| 工具 | 作用 | 是否消耗积分 |
|------|------|--------------|
| `hunyuan_health` | 代理健康：上游、模式、端口、签名键（**不含账号信息**） | 否 |
| `hunyuan_set_mode` | 切换默认上游 `main` / `concept` | 否 |
| `hunyuan_assets` | 资源看板：任务状态/进度/模型下载链接（已裁剪） | 否 |
| `hunyuan_works` | 概念站 pipeline 原始视图，可按 `worksId` 过滤 | 否 |
| `hunyuan_main_creations` | 主站 creations 列表 | 否 |
| `hunyuan_generate_concept` | **概念站文生3D**（产出白模） | **是** |
| `hunyuan_generate_main` | **主站文生3D**（上游固定 4 个候选） | **是** |
| `hunyuan_texture` | **给白模加纹理** → 带 PBR 的成品 GLB | **是** |

### MCP 不暴露账号能力（设计约束）

以下能力**故意不提供**给 MCP 客户端，只能在 Web 控制台「登录/积分」面板操作：

| 未暴露 | 原因 |
|--------|------|
| 导入 Cookie（`hunyuan_import_cookies`） | 可注入任意会话，等于接管账号 |
| 登出（`hunyuan_logout`） | 可破坏他人会话 |
| 查询积分（`hunyuan_credits`） | 账号额度信息 |
| 查询登录态（`hunyuan_login_status`） | 会话/身份状态 |

`hunyuan_health` 也**不再返回** `cookieCount` / `sessionLike` —— 那两个字段描述的是账号而非服务。

**接入 MCP 前请先在 Web 控制台完成登录并确认额度。**

### 典型调用顺序

```
（先到 Web 控制台登录、确认额度 —— MCP 侧无此能力）
hunyuan_generate_concept(prompt="可爱的卡通机器人玩具，白底", kind="geometry")
hunyuan_works(worksId="<上一步的 worksId>")                      → 轮询 pipelineStatus 1→2
hunyuan_texture(worksId="<同上的 worksId>")                       → 提交纹理
hunyuan_works(worksId="<纹理返回的 worksId>")                     → 完成后取
                                                                  texturePaintingRsp.glbUrl
```


## 行为约定

- **异步任务**：两个生成工具只负责提交，立刻返回；进度和产物一律用 `hunyuan_assets` / `hunyuan_works` / `hunyuan_main_creations` 查。
- **参数校验前置**：`prompt` 为空、`mode` 非法、Cookie 为空等，会在**发起上游请求之前**返回 `isError`，不会白扣积分。
- **输出裁剪**：资源看板原始响应约 200KB（24 条），工具会裁剪成 `{count,totalCount,items[]}`，只保留
  `source/id/title/status/progress/previewUrl/primaryModelUrl/error/assetCount`；单次输出上限 128KB，超出会截断并标注 `truncated`，避免撑爆模型上下文。
- **错误语义**：工具内的失败（上游 4xx/5xx、代理不可达）返回 `isError: true` + 可读文本，而不是 JSON-RPC 错误；
  协议级错误才用 JSON-RPC error（`-32700` 解析失败、`-32601` 未知方法、`-32602` 未知工具/参数非法）。
- **脱敏**：沿用 HTTP 层的 `scrubSensitive`/`maskID`，手机号、token、`innerUserId` 不会经 MCP 泄漏。

## HTTP 调用示例

默认（未设令牌）：

```sh
curl -s http://127.0.0.1:8812/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{}}}'
```

开启令牌后需带上请求头（令牌即 `MCP_HTTP_TOKEN` 的值）：

```sh
curl -s http://127.0.0.1:8812/mcp \
  -H "Authorization: Bearer $MCP_HTTP_TOKEN" -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

`GET /mcp` 是能力探测（返回工具数，不消耗积分）；`GET /api/mcp/panel` 是控制台面板数据源。
通知类消息（如 `notifications/initialized`）返回 **202 空响应体**。

## 手工联调

```sh
curl -s http://127.0.0.1:8812/mcp -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' | python3 -m json.tool
```

`go test ./...` 覆盖协议握手、通知无响应（202）、工具目录校验、错误码、输出裁剪、参数校验、
HTTP 鉴权开关与跨源拒绝（共 20 个 MCP 测试，全部离线、in-process 驱动）。
