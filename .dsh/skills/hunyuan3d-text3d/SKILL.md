---
name: hunyuan3d-text3d
description: Use when generating, iterating on, or debugging 3D models from text with this project's Hunyuan3D proxy — writing prompts that produce clean geometry, choosing between concept/main platforms and their independent credit pools, picking modelVersion/faceCount/kind/style, polling async jobs to completion, diagnosing bad output (blobby shapes, shredded meshes, wrong style, missing textures), retrieving GLB/FBX URLs, and avoiding wasted credits. Drives the built-in MCP server's tools (hunyuan_generate_concept, hunyuan_texture, hunyuan_works, ...) over its HTTP endpoint.
---

# 混元3D 文生3D 建模流程

本仓库是腾讯混元3D 的反代服务器（**仅学习用途**）。生成**消耗真实积分**且是**异步**的：
提交只拿到任务 id，必须轮询才能拿到模型。

**核心心智模型**：这是一个"**提交 → 等待 → 判断 → 迭代**"的循环，不是一个同步函数。

**关键：几何生成只产出白模（无材质），不是成品。**
`kind="geometry"` 拿到的 GLB 体积约 0.3MB、`materials=0`；要得到带 PBR 贴图的成品，
必须再走一步**纹理**（见「完整建模流程」）。只跑到白模就说"完成"是错的。

## 决策树：先搞清楚要做什么

| 情境 | 做法 |
|------|------|
| 该用 main 还是 concept？ | **先读下面「main 还是 concept？」章节**，选错等于白花积分 |
| 第一次用 / 不知道能不能跑 | 先看 `hunyuan_health` 确认服务在线；**登录与额度去 Web 控制台看** |
| 要一个**带纹理的成品模型**（最常见） | **`hunyuan_generate_main` 一步生成**（见「路线 A」） |
| 要控制面数/版本，或要后续管线加工 | `hunyuan_generate_concept(kind="geometry")` → `hunyuan_texture`（路线 B） |
| 给已有白模补纹理 | `hunyuan_texture(worksId="<该作品>")` |
| 只要设计图 / 概念图（快、省） | `hunyuan_generate_concept(kind="design")` → 拿 `view_*ImageUrl` 四视图 |
| 先出图再定稿再出模型 | `kind="both"`（两步都跑，扣分更多） |
| 要多个候选里挑一个 | `hunyuan_generate_main`（上游固定生成 **4 个候选**） |
| 查进度 / 拿下载链接 | `hunyuan_assets(source="concept")` 或 `hunyuan_works(worksId=...)` |
| 模型不对，要改 | 读下面「质量诊断与迭代」再决定改什么 |
| 需要接口细节/排错 | 见 `docs/mcp.md` 与 `docs/proxy-api.md` |


## main 还是 concept？（先决定这个）

腾讯混元3D 有**两个独立平台**，接口、积分、能力都不同。**选错等于白花积分。**

| | **concept**（概念站） | **main**（主站） |
|---|---|---|
| 生成工具 | `hunyuan_generate_concept` | `hunyuan_generate_main` |
| 查询工具 | `hunyuan_assets(source="concept")` / `hunyuan_works` | `hunyuan_main_creations` |
| 积分池 | `remainingTimes` | `remainQuota` |
| 单次产出 | 按 `kind` 出设计图 / 几何 | **固定 4 个候选** |
| 后续加工 | ✅ 纹理、UV、分割、低模、绑定、动作 | ❌ 基本没有 |
| 风格枚举 | `hand_drawn` `q_version` `steampunk` `futuristic` `pixel` `low-poly` | `cartoon` `stone` `celadon` `chinese` `cyberpunk` `hand_drawn` |
| 模型版本 | `v3.0` `v3.1` `v3.5-preview` | `v3.1` `v3.5` |

### 怎么选

**用 concept**（绝大多数情况）：
- 要**可交付的 3D 模型文件**（GLB/FBX）
- 要**加纹理**、要控制面数、要后续管线加工
- 要精细控制风格/版本

**用 main**：
- 只想**快速看几个方案**挑一个（一次给 4 个候选）
- 不需要后续纹理/加工
- concept 积分用完了但 main 还有（两池独立）

### 重要：不需要先切 mode

`hunyuan_generate_concept` / `hunyuan_generate_main` **各自绑定平台**，
直接调用即可，**不用先 `hunyuan_set_mode`**。

`hunyuan_set_mode` 只影响**未显式指定 mode 的裸 HTTP 透传**
（例如透传任意上游路径时）。

### 积分要分别看

两个池子**完全独立**：

```
main    → remainQuota       （在 Web 控制台顶部徽章看）
concept → remainingTimes    （同上）
```

concept 剩 0 不代表 main 不能用，反之亦然。**MCP 不提供额度查询**，去控制台看。

### 风格名不通用（易错）

两站的 `style` 枚举**完全不同**，混用会被拒绝：

```
concept 传 cartoon  → 报错
main    传 low-poly → 报错
```

本地已做严格校验，会直接告诉你合法取值。

## 前置检查（每次都要做，别跳过）

**MCP 不暴露任何账号能力**（无登录状态、无积分查询、无 Cookie 导入/登出）。
登录与额度必须在 **Web 控制台** http://127.0.0.1:8812/ 完成：

- 未登录 → 控制台「登录」面板微信扫码（QQ 扫码已废弃，可用 Cookie 导入）
- 额度 → 控制台顶部徽章显示 main / concept 剩余

**积分是独立的两个池**：concept 走 `remainingTimes`，main 走 `remainQuota`，互不通用。
在 concept 查到的额度不代表 main 能用。`hunyuan_health` 只能确认服务在线，不含账号信息。

## 生成流程：先选路，别一律两阶段

**两种走法，选错会多花一倍积分。**

### 路线 A：main 一步生成（默认选这个）

一个提示词直接出**带纹理的成品**，还一次给 4 个候选。

```
① hunyuan_generate_main(prompt="...", style="cartoon", faceCount=300000)
      → 立即返回 creationsId；消耗 1 积分
② 轮询 hunyuan_main_creations(limit=4) 直到 status=success
③ 取候选的产物：
      urlResult.textureGlb              ← ★带纹理的成品 GLB
      urlResult.pbrImage                ← 基础色贴图
      urlResult.pbrNormalImage / pbrMetallicImage / pbrRoughnessImage
      urlResult.geometryGlb             ← 白模（若只要几何）
      urlResult.image_url               ← 渲染预览
```

**提示词简短清晰时（"一只坐姿的狮子，金黄色鬃毛"）就走这条。**
实测：4 个候选全部成功，成品 37.5MB、`materials=1`、3 张贴图。

⚠️ 产物在 `result[].urlResult`，不是顶层字段。`modelUrl3d` 往往是空的，别据此判断失败。

### 路线 B：concept 两阶段（需要精细控制时）

要控制面数/风格/版本，或要用同一几何做多次纹理时走这条。代价：**两次提交 = 两次扣分**。

```
【阶段一：几何】
① hunyuan_generate_concept(prompt="...", kind="geometry")
      → 记下 text2geometry.worksId；消耗 1 积分
② 轮询 hunyuan_works(worksId="<①的 worksId>")   间隔 15s
      → pipelineStatus: 1=生成中, 2=完成, 3=失败
③ 完成 resultUrls.geometryGenerationRsp.glbUrl    ← 白模（materials=0）

【阶段二：纹理】
④ hunyuan_texture(worksId="<①的 worksId>", method="image")
      → 传 worksId 即可，工具自动取 fbxUrl + imageUrl 并补全上游必需字段
      → method="text" 可用文字描述材质（prompt="青铜铸造，做旧金属质感"），
        但有 UV 前提，见下方 ⚠️
      → 消耗 1 积分
⑤ 轮询 hunyuan_works(worksId="<④返回的 worksId>")
⑥ 完成 resultUrls.texturePaintingRsp.glbUrl     ← ★带 PBR 贴图的成品
      + pbrImageUrl / pbrNormalImageUrl / pbrMetallicImageUrl / pbrRoughnessImageUrl
```

⚠️ **method="text" 的 UV 前提（实测踩坑）**：文生纹理要求 fbx **已 UV 展开**。

| 任务状态 | method="text" 结果 |
|----------|--------------------|
| 全新白模（从没跑过纹理） | **跑完但零产物**（无报错，texturePaintingRsp 缺失） |
| 已跑过一次 image 纹理（有 UV） | ✅ 正常出贴图 |

所以对**全新白模**想用文字定制材质时：先 `method="image"` 产出 UV 和贴图，
再 `method="text"` 用文字重新上材质。工具在任务已有纹理产物时会自动选用
已 UV 的 fbx，无需手工传。

### 怎么选

| 情况 | 走哪条 |
|------|--------|
| 提示词简短清晰，就要一个模型 | **A（main）** |
| 想从几个候选里挑 | **A（main）** |
| 要控制 `faceCount` / `modelVersion` | B（concept） |
| 要低模、要后续 UV/分割/绑定/动作 | B（concept） |
| 同一个几何要试多种纹理 | B（concept） |

### 成品验收（两条路通用）

**必须检查 GLB 内的 `materials > 0`**：

| | 白模 | 成品 |
|---|---|---|
| materials / images / textures | 0 / 0 / 0 | 1 / 3 / 3 |
| 大小 | 0.3–5 MB | 16–38 MB |

只看"有 glbUrl"不够 —— 白模同样有 glbUrl。

**耗时**：每阶段约 2–3 分钟（`pipelineStatus` 1→2）。不到 3 分钟不要判定失败。

## 提示词方法论（决定成败的一步）

上游对提示词的**结构**比"文采"敏感得多。按 **主体 + 风格 + 材质 + 背景** 组织：

| 槽位 | 作用 | 写法示例 |
|------|------|----------|
| **主体** | 决定几何形态 | "一只坐姿的柯基犬" |
| **风格** | 决定体块与轮廓 | "低多边形" / "卡通圆润" / "写实" |
| **材质** | 决定表面颜色 | "木质" / "陶瓷" / "金属" |
| **背景** | 必须写，否则易出杂物 | **"白色背景"** |
| **排除项** | 防文字/水印被建模 | **"无文字"** |

**好例子**：
> 一个低多边形风格的木质小台灯，暖色灯罩，白色背景，无文字

**坏例子**：
> 台灯
（缺少风格/材质/背景 → 几何松散，可能把背景杂点也建进去）

**坏例子**：
> 一个非常精美的、充满设计感的、艺术化的台灯，要有多边形感，还要有点像木头，
> 灯罩希望是暖色的，最好再带一点复古感觉
（口语化长句、多重要求互相冲突 → 上游无法解析成明确形体）

要点：

- **具体名词 > 抽象形容**。"柯基犬" 优于 "可爱的动物"。
- **风格词要少而准**。堆叠多个风格（"低多边形+写实+赛博朋克"）会互相抵消。
- **别让它建模"细碎结构"**。链条、镂空、大量薄片、飘带、毛发、文字 —— 这些
  在 3D 生成里最容易变成糊成一团的碎面。要么避开，要么接受粗略形态。
- **单个主体**。多主体（"一只猫和一张桌子"）常被融成一个整体。
- **要贴合参考图效果**时，用 `kind="design"` 先出四视图，定稿后再用同提示词出几何。

## 参数决策表

| 参数 | 取值 | 何时用 |
|------|------|--------|
| `kind` | `geometry` | 要 GLB 模型（**默认选这个**） |
| | `design` | 只要设计图/四视图，快且便宜 |
| | `both` | 先出图再出几何；扣分更多，仅确定要两样时用 |
| `modelVersion` | `v3.1` | 默认，稳定 |
| | `v3.0` | 老版本，一般不必用 |
| | `v3.5-preview` | 更新版本，效果可能更好但属 preview |
| `faceCount` | 默认 `500000` | 通用 |
| | `30000`–`100000` | 要低模 / 快 / 文件小（游戏资产、网页展示） |
| | `1000000`+ | 要保留细节（雕刻感），文件更大、更慢 |
| `style` | `hand_drawn`(默认)/`q_version`/`steampunk`/`futuristic`/`pixel`/`low-poly` | **只影响设计图外观，不影响几何质量** |
| `apose` | `true` | 角色类模型，要 A-pose 便于后续绑定 |

**注意**：`faceCount` 控制的是网格密度，**不修复糟糕的形体**。
形状本身不对时，改提示词，不是改面数。

主站（`hunyuan_generate_main`）的 `style` 是**另一套**贴图风格枚举：
`cartoon` / `stone` / `celadon` / `chinese` / `cyberpunk` / `hand_drawn`。
**两站风格名不通用**，混用会被拒绝（见下）。

## 质量诊断与迭代

模型出来后，按现象定位原因：

| 现象 | 可能原因 | 改什么 |
|------|----------|--------|
| 形体糊成一团、像融化 | 提示词太抽象 / 多主体 / 细碎结构 | 换具体单一主体；避开链条毛发镂空 |
| 表面有洞、法线乱、碎面 | 细薄结构超出发明能力 | 简化结构；提高 `faceCount` 试一次 |
| 混进了背景杂物 | 提示词没写背景 | 加"白色背景" |
| 模型上出现文字/水印 | 提示词没排除 | 加"无文字" |
| 风格和想象不符（但形状对） | `style` 只影响设计图 | 若形状对则无需改；想改外观用 `kind="design"` 重出 |
| 形状对但细节少 | 面数过低 | 提高 `faceCount` |
| 文件太大 / 太慢 | 面数过高 | 降低 `faceCount` |
| `pipelineStatus` 长时间停在 1 | 上游排队或任务卡住 | 等满 3–5 分钟；仍不动则重新提交（**会再扣分**） |
| 返回 `text2geometry.error` | 上游明确拒收 | 读错误原文，通常是提示词或参数问题 |

**迭代原则**：

1. **一次只改一个变量**，否则无法归因。
2. **改提示词优先于改参数**。形体问题 90% 是提示词问题。
3. **每次迭代都扣分**。先用 `kind="design"` 验证构思（便宜），定稿后再 `geometry`。
4. 已经在跑的任务**不要重复提交**，先查状态。

## 常见坑（都踩过）

- **提交成功 ≠ 生成成功。** 返回里只有 `worksId`，没有模型。必须轮询。
- **`hunyuan_assets` 的 `primaryModelUrl` 可能是图片**（设计类任务），不是模型。
  要模型看 `hunyuan_works` 的 `geometryGenerationRsp.glbUrl`，或直接按 `kind="geometry"` 提交。
- **`hunyuan_works` 按 `worksId` 过滤**：未命中返回 `count=0`，属正常（该页没有）。
- **积分按提交次数扣**，与成功与否无关。失败重试**再扣一次**。
- **`style` 传非法值会被拒绝**（不是静默改成默认）。两站的合法值不同，见参数表。
- 上游偶发 `500`（如 `planCreditInfo`）**不影响**生成主流程。

## MCP 接入

MCP 只有 **HTTP 一种传输**，随 HTTP 服务自动启用：

```
端点   POST http://127.0.0.1:8812/mcp   （Streamable HTTP）
协议   2025-06-18
鉴权   默认不校验；设 MCP_HTTP_TOKEN 后需 Authorization: Bearer <token>
```

**MCP 依附于 HTTP 服务** —— 服务没起来 `/mcp` 就不可用。

客户端配置：

```json
{"mcpServers":{"hunyuan3d":{"url":"http://127.0.0.1:8812/mcp"}}}
```

若工具不可用，先确认 HTTP 服务在跑（MCP 依附于它）；
Web 控制台 http://127.0.0.1:8812/ 的「MCP 服务」面板可看状态与工具目录。

## 参考文件

- `references/prompt-patterns.md` — 更多提示词模板与按品类（角色/道具/场景）的写法
- `references/troubleshooting.md` — 更细的排错流程与上游错误对照
- `docs/mcp.md` — MCP 工具目录、鉴权与状态
- `docs/proxy-api.md` — 本地 API 完整目录
- `mcp_tools.go` — 工具实现与输出裁剪逻辑
