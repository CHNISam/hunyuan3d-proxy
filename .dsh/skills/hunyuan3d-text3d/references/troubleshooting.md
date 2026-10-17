# 排错手册

配合主 `SKILL.md` 的「质量诊断与迭代」使用。按**症状**查。

## 阶段 0：任务根本没提交成功

| 症状 | 原因 | 处理 |
|------|------|------|
| `error: prompt is required` | prompt 为空或全空白 | 传非空提示词 |
| `error: mode must be main or concept` | 模式名非法 | 只用 `main` / `concept` |
| `error: style="xxx" 不是合法取值` | 风格名不在该站枚举内 | 见主 SKILL 参数表；**两站风格名不通用** |
| `error: provide raw, or hy_user + hy_token` | 导入 Cookie 时没给内容 | 补 `raw` 或 `hy_user`+`hy_token` |
| `proxy unreachable` | 服务没起 / 地址不对 | 启动服务，检查端口 |

## 阶段 1：提交成功但拿不到模型

提交返回里**只有** `requestId` 和 `text2geometry.worksId`，**没有模型** —— 这是正常的。

| 症状 | 判断 | 处理 |
|------|------|------|
| `pipelineStatus: 1` | 生成中 | 等，间隔 15s；**3 分钟内不要判定失败** |
| `pipelineStatus: 2` 且 `resultUrls` 有值 | 完成 | 取 `geometryGenerationRsp.glbUrl` |
| `pipelineStatus: 2` 但 `resultUrls` 为空 | 完成但无产物 | 看 `errorMsg` / `errorCode` |
| `text2geometry.error` 非空 | 上游提交即拒 | 读原文，通常是提示词或参数 |
| 停在 1 超过 5 分钟 | 卡住/排队 | 重新提交，**会再扣一次积分** |

**状态码对照**：`pipelineStatus` 1=生成中，2=完成。`status` 与它同步。

## 阶段 2：模型不理想（形体/质量）

| 现象 | 根因 | 处理 |
|------|------|------|
| 糊成一团、像融化 | 提示词抽象 / 多主体 | 具体单一主体，见 `prompt-patterns.md` |
| 表面破洞、碎面、法线乱 | 细薄结构 | 简化结构；试提高 `faceCount` |
| 背景杂物被建模 | 没写背景 | 加"白色背景" |
| 表面有文字噪点 | 没排除文字 | 加"无文字" |
| 形状对但太粗糙 | 面数低 | 提高 `faceCount` |
| 文件过大 / 下载慢 | 面数高 | 降到 30000–100000 |
| 细节缺失 | 面数或版本 | 提高 `faceCount`；试 `v3.5-preview` |
| 外观风格不符 | `style` 只影响设计图 | 形状对就无需改；要换外观用 `kind="design"` |

**关键区分**：`faceCount` 改的是**网格密度**，不会修复错误形体。
形体不对 → 改提示词。

## 阶段 3：环境与账号

| 症状 | 处理 |
|------|------|
| 未登录 | **只能去 Web 控制台**扫码或导入 Cookie（MCP 不暴露账号能力） |
| 额度不够 | concept 与 main **额度独立**，查对应的那个 |
| 上游偶发 500（如 `planCreditInfo`） | **不影响**生成主流程，忽略 |
| `/mcp` 连不上 | MCP 依附 HTTP 服务，先确认服务在跑 |

## 成本纪律

- 每次提交都扣分，**无论成功失败**；重试再扣。
- 迭代先用 `kind="design"`（便宜）验证构思，定稿再 `kind="geometry"`。
- 提交前先在 **Web 控制台**确认已登录且有额度（MCP 查不到）。
- 已有任务在跑时先查状态，别重复提交。

## 阶段 4：纹理（拿到带贴图成品）

几何只是白模。要成品必须走纹理阶段，实测要点：

### 选择 texture 的 method

`hunyuan_texture(worksId="...", method=...)`：

| method | 适用 | 实测 |
|--------|------|------|
| `image`（默认） | 用该作品的渲染图生成贴图 | ✅ 5/5 |
| `text` | **用文字描述材质**（需 `prompt`） | ✅ 已成功 |
| `views` | 四视图生成（需 `viewsImage`） | ✅ 1/1 |

传 `worksId` 即可，工具自动推导输入。

### 文生纹理（method=text）的两个坑

**坑 1：工具内部会走对的路，但你得知道为什么。**
上游有两个看着都像"文生纹理"的端点，只有一个能用：

| 上游端点 | 结果 |
|----------|------|
| `texture_edit_local` (editType=1) | ✅ 走 `texture-edit-text`，成功 |
| `text2texture_v31` | ❌ 走 `texture-preprocess`，**7/7 全失败** |

`hunyuan_texture(method="text")` 用的是前者。若你自己拼 HTTP，别选错。

失败的那个稳定返回：

```
errorCode: 400
errorMsg: "http client codec StatusCode: Bad Request"
pipelineStatus: 3
resultUrls: {}
```

对比模型链可以看清根因——只有它带 preprocess：

| 路径 | model 链 |
|------|----------|
| `image2texture` | segmentation → image2texture ✅ |
| `views2texture` | segmentation → views2texture → texture-edit → image2texture ✅ |
| `text2texture` | **texture-preprocess** → segmentation → image2texture ❌ |

补 `imageUrl` / 换 `worksType` / 换版本都没用，**依旧卡在同一步**。这是上游该环节的问题。

**坑 2：fbxUrl 必须已 UV 展开。** 传几何白模的 fbx 会"跑完但不产出贴图"（无报错，`texturePaintingRsp` 缺失）：

| 传入 | 结果 |
|------|------|
| `geometryGenerationRsp.fbxUrl`（白模） | 完成，但无 `texturePaintingRsp` |
| `texturePaintingRsp.fbxUrl`（已 UV） | ✅ 完整贴图 |

`hunyuan_texture(method="text")` **已自动优先选用已 UV 展开的 fbx**，直接用 `worksId` 即可。
但**全新白模没有 UV fbx 可选** —— 此时 `method="text"` 就会零产物。
正确顺序：先 `method="image"`（产出 UV 与贴图），再用 `method="text"` 以文字重上材质。

### 提交参数（文档不全，实测得来）

文档只写 `{fbxUrl, prompt, dependOnWorksId}`，这样提交会得到 `200002 输入参数错误`。必须补：

```json
{
  "fbxUrl": "...", "imageUrl": "...", "dependOnWorksId": "<几何的 worksId>",
  "worksType": 2, "requestId": "go<12位随机>", "modelVersion": "v3.1", "keepUv": false
}
```

### 成品验收标准

下载 `texturePaintingRsp.glbUrl` 后检查 glTF 头：

| 指标 | 白模 | 成品 |
|------|------|------|
| materials | 0 | 1 |
| images / textures | 0 / 0 | 3 / 3 |
| 文件大小 | ~0.3 MB | ~16 MB |

**只看"有 glbUrl"不够** —— 白模也有 glbUrl。必须看 `materials > 0`。

### 未验证项

- `keepUv`：已实测 `true` / `false` 两种取值都能出完整成品（GLB 均含 `TEXCOORD_0`、16.4MB）。
- `views2texture` 的 `viewsImage` 需四视图拼接，格式未测。
