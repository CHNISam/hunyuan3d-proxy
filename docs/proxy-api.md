# hunyuan3d-proxy API 文档

> 官方腾讯云混元生3D API 文档（**参数、错误码、限频以官网为准**）：
> - 简介：https://cloud.tencent.com/document/product/1804/120829
> - API 概览：https://cloud.tencent.com/document/product/1804/120838
> - 公共参数：https://cloud.tencent.com/document/api/1804/120832
> - 签名方法 v3（TC3-HMAC-SHA256）：https://cloud.tencent.com/document/api/1804/120833
> - 提交专业版任务：https://cloud.tencent.com/document/api/1804/123447
> - 查询专业版任务：https://cloud.tencent.com/document/api/1804/123448
> - Go SDK：https://github.com/TencentCloud/tencentcloud-sdk-go/tree/master/tencentcloud/ai3d/v20250513

本服务默认：`http://127.0.0.1:8787`

---

## 三套入口

| 入口 | 鉴权 | 用途 |
|------|------|------|
| **main** 网页反代 | Cookie / 扫码导入（hy_user 等） | 3d.hunyuan.tencent.com 主站 `/api/3d/*` |
| **concept** 网页反代 | 同上 Cookie | `/studio/creation/concept` 的 `/game3d/*`、`/3d/*` |
| **cloud** 官方 API | SecretId/SecretKey + TC3 | `ai3d.tencentcloudapi.com`，Version `2025-05-13` |

---

## 元信息

### GET /api/health
### GET/POST /api/mode?mode=main|concept
### GET /api/sign/debug

网页端签名（模块 47436）：

- 键：`Hf6d6KFB3D`
- 算法：`Hex(HmacSHA256(canonical, key))`
- 字段：query 上的 `timestamp` / `nonce` / `sign`
- **仅主站** `POST /api/3d/creations/generations` 等主站接口需要；concept `/game3d/*` 不加签

---

## 登录

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/login/qr/start` | 微信 WxLogin 授权 URL + ticket |
| GET | `/api/login/qr/status?ticket=` | 轮询 |
| GET | `/api/login/status` | 会话状态 |
| POST | `/api/login/cookies` | 导入浏览器 Cookie |
| POST | `/api/login/logout` | 清空 |
| GET | `/api/jointappid` | 联合登录 appId |
| GET | `/api/open/auth` | OAuth 回调换会话 |

Cookie 键：`hy_user`、`hy_token`、`userInfo`、`hy_device_id`；头 `x_hunyuan_inner_user_id`。

---

## 积分（两端独立）

| 路径 | 上游 |
|------|------|
| GET `/api/credit/main` | 主站 `POST /api/3d/quotainfo` + `GET /api/3d/planCreditInfo` |
| GET `/api/credit/concept` | `POST /game3d/general_info/get_remaining_times` |
| GET `/api/credit/all` | 分别查询，互不影响 |

也可直接透传：

```text
POST /api/3d/quotainfo
GET  /api/3d/planCreditInfo
POST /game3d/general_info/get_remaining_times
```

---

## 网页端 main 功能代理

```text
GET  /api/3d/config
POST /api/3d/resource/genUploadInfo
POST /api/3d/creations/generations          # 自动签名
GET  /api/3d/creations/detail
POST /api/3d/creations/retopologize
POST /api/3d/creations/resourceConvert
POST /api/3d/creations/boneBindingFromMesh
POST /api/3d/creations/manualBoneBinding*
POST /api/3d/creations/count|delete|cancel
POST /api/3d/resource/review|cmykCheck
POST /api/3d/feedback/submit
GET  /api/3d/planCreditInfo
POST /api/3d/quotainfo
POST /api/3d/purchaseBasicPlan
GET  /api/3d/purchaseList
GET  /api/3d/interaction/config
GET  /api/3d/worldPlay/config
POST /api/3d/worldPlay/text2Image|page2video
POST /api/3d/worldScene/review
POST /api/3d/notice/list|statusUpdate
```

请求头由代理注入：`x-source: web`、`x-product: hunyuan3d`。

---


## 选 main 还是 concept？

两个平台**接口、积分、能力都独立**，选错会白花积分。

| | **concept**（概念站） | **main**（主站） |
|---|---|---|
| 本地生成端点 | `POST /api/text3d/generate` | `POST /api/text3d/main` |
| 上游前缀 | `/game3d/*` | `/api/3d/*` |
| 积分 | `POST /game3d/general_info/get_remaining_times` | `POST /api/3d/quotainfo` |
| 单次产出 | 按 `mode` 出设计图/几何 | **固定 4 个候选**（`count=1` 被上游拒绝） |
| 后续加工 | ✅ 纹理/UV/分割/低模/绑定/动作 | ❌ 基本没有 |
| `style` 枚举 | `hand_drawn` `q_version` `steampunk` `futuristic` `pixel` `low-poly` | `cartoon` `stone` `celadon` `chinese` `cyberpunk` `hand_drawn` |
| 模型版本 | `v3.0` `v3.1` `v3.5-preview` | `v3.1` `v3.5` |
| 签名 | 不需要 | 部分接口需要（见 `docs/signature.md`） |

**选择建议**

- 要**可交付模型**、要**加纹理**、要控制面数或走后续管线 → **concept**
- 只想**快速拿几个候选**挑一个、不需要加工 → **main**
- 两个池子独立，一边用完可换另一边

**`style` 名不通用**：`concept` 传 `cartoon`、`main` 传 `low-poly` 都会被拒绝
（本地已做严格校验并报出合法取值）。

`POST /api/mode?mode=main|concept` 只影响**未显式指定 mode 的裸透传**；
`/api/text3d/generate` 恒走 concept，`/api/text3d/main` 恒走 main。

## 网页端 concept 功能代理

```text
GET  /3d/getuserinfo
POST /game3d/general_info/*
POST /game3d/resource/*
POST /game3d/concept_design/text2image|image2views
POST /game3d/geometry_generation/*
POST /game3d/component_splitting/*
POST /game3d/low_poly_topology/*
POST /game3d/uv_unwrapping/uv_unwrapping
POST /game3d/texture_painting/*
POST /game3d/bone_skinning/bone_skinning
POST /game3d/motion_generation/*|motion_retarget/*
POST /game3d/share/create
GET  /game3d/share/detail
POST /new-portal/whitelist/apply|query
```

别名：`/api/concept/game3d/...`  
Catch-all：`/game3d/*`、`/new-portal/*`、`/3d/*`、`/api/concept/*`、`/proxy/concept/*`

Concept 响应包络：`{errNo, errMsg, data}`，`errNo===0` 成功。

---

## 腾讯云官方 ai3d API 代理

鉴权：环境变量 `TENCENT_SECRET_ID` / `TENCENT_SECRET_KEY`（或请求头 `x-secret-id` / `x-secret-key`）。

Endpoint：`https://ai3d.tencentcloudapi.com`  
Version：`2025-05-13`  
Region 默认：`ap-guangzhou`（可用 `?region=` 或 `TENCENT_REGION`）  
签名：TC3-HMAC-SHA256（本服务自动完成）

### Action 映射表

与官网 [API 概览](https://cloud.tencent.com/document/product/1804/120838) 一致：

| 本地 slug | Action | 文档 |
|-----------|--------|------|
| `submit-pro` | SubmitHunyuanTo3DProJob | [123447](https://cloud.tencent.com/document/api/1804/123447) |
| `query-pro` | QueryHunyuanTo3DProJob | [123448](https://cloud.tencent.com/document/api/1804/123448) |
| `submit-rapid` | SubmitHunyuanTo3DRapidJob | [123463](https://cloud.tencent.com/document/api/1804/123463) |
| `query-rapid` | QueryHunyuanTo3DRapidJob | [123464](https://cloud.tencent.com/document/api/1804/123464) |
| `submit-texture` | SubmitTextureTo3DJob | [126292](https://cloud.tencent.com/document/api/1804/126292) |
| `query-texture` | DescribeTextureTo3DJob | [126297](https://cloud.tencent.com/document/api/1804/126297) |
| `submit-reduce-face` | SubmitReduceFaceJob | [126293](https://cloud.tencent.com/document/api/1804/126293) |
| `query-reduce-face` | DescribeReduceFaceJob | [126298](https://cloud.tencent.com/document/api/1804/126298) |
| `submit-part` | SubmitHunyuan3DPartJob | [126295](https://cloud.tencent.com/document/api/1804/126295) |
| `query-part` | QueryHunyuan3DPartJob | [126296](https://cloud.tencent.com/document/api/1804/126296) |
| `submit-uv` | SubmitHunyuanTo3DUVJob | [126294](https://cloud.tencent.com/document/api/1804/126294) |
| `query-uv` | DescribeHunyuanTo3DUVJob | [126299](https://cloud.tencent.com/document/api/1804/126299) |
| `submit-motion` | SubmitHunyuanTo3DMotionJob | [131256](https://cloud.tencent.com/document/api/1804/131256) |
| `query-motion` | DescribeHunyuanTo3DMotionJob | [131257](https://cloud.tencent.com/document/api/1804/131257) |
| `submit-rigging` | SubmitAutoRiggingJob | [131618](https://cloud.tencent.com/document/api/1804/131618) |
| `query-rigging` | DescribeAutoRiggingJob | [131619](https://cloud.tencent.com/document/api/1804/131619) |
| `submit-profile` | SubmitProfileTo3DJob | [127685](https://cloud.tencent.com/document/api/1804/127685) |
| `query-profile` | DescribeProfileTo3DJob | [127686](https://cloud.tencent.com/document/api/1804/127686) |
| `convert-format` | Convert3DFormat | [126300](https://cloud.tencent.com/document/api/1804/126300) |

通用入口：

```text
GET  /api/cloud
GET  /api/cloud/actions
POST /api/cloud/action/{ActionName}
```

### SubmitHunyuanTo3DProJob 常用输入字段（摘自官网，详见文档）

| 参数 | 类型 | 说明 |
|------|------|------|
| Model | String | `3.0` / `3.1`（3.1 时 LowPoly 不可用） |
| Prompt | String | 文生3D，≤1024 utf-8 字符 |
| ImageBase64 / ImageUrl | String | 图生3D，与 Prompt 二选一 |
| MultiViewImages.N | Array | left/right/back/top/... |
| EnablePBR | Boolean | 默认 false |
| FaceCount | Integer | 3000–1500000，默认 500000 |
| GenerateType | String | Normal / LowPoly / Geometry / Sketch |
| PolygonType | String | triangle / quadrilateral（LowPoly） |
| ResultFormat | String | 可选 STL/USDZ/FBX |

输出：`JobId`（24h 有效）、`RequestId`。

### 示例

```bash
# 列出云端能力
curl http://127.0.0.1:8787/api/cloud

# 提交专业版
curl -X POST http://127.0.0.1:8787/api/cloud/submit-pro \
  -H "Content-Type: application/json" \
  -d '{"Prompt":"一只小猫","Model":"3.0","GenerateType":"Normal"}'

# 查询
curl -X POST http://127.0.0.1:8787/api/cloud/query-pro \
  -H "Content-Type: application/json" \
  -d '{"JobId":"1357237233311637504"}'
```

响应包络：

```json
{
  "ok": true,
  "source": "tencent-cloud-ai3d",
  "action": "SubmitHunyuanTo3DProJob",
  "docs": "https://cloud.tencent.com/document/product/1804/120838",
  "data": { "Response": { "JobId": "...", "RequestId": "..." } }
}
```

---

## 文生 3D（Go 反代）

### 主站 main（扣主站积分 remainQuota）

Playwright 实测成功 body（**不是** configInfo 字段；服务端按 `modelType` 找配置）：

```json
POST /api/3d/creations/generations?timestamp=&nonce=&sign=
{
  "sceneType": "playGround3D-2.0",
  "count": 4,
  "modelType": "text2ModelV3.1",
  "title": "<prompt>",
  "style": "cartoon",
  "prompt": "<prompt>",
  "enable_pbr": true,
  "enableLowPoly": false,
  "faceCount": 500000
}
```

Go 封装：

```bash
# UTF-8 JSON body
curl -X POST http://127.0.0.1:8810/api/text3d/main \
  -H "Content-Type: application/json; charset=utf-8" \
  -d '{"prompt":"紫色的小火箭，卡通风格","style":"cartoon","modelVersion":"v3.1","faceCount":500000}'

curl -X POST http://127.0.0.1:8810/api/text3d/main/list \
  -H "Content-Type: application/json" \
  -d '{"limit":5,"offset":0,"sceneTypeList":[]}'
```

实测：`creationsId` 返回 200，`remainQuota` 20→17。

### Concept（扣 concept remainingTimes）

```bash
curl -X POST http://127.0.0.1:8810/api/text3d/generate \
  -H "Content-Type: application/json" \
  -d '{"prompt":"...","mode":"both","modelVersion":"v3.1","faceCount":500000,"style":"hand_drawn"}'
```

### 官方云 API 文生 3D

```bash
# 需 TENCENT_SECRET_ID/KEY
curl -X POST http://127.0.0.1:8810/api/cloud/submit-pro \
  -H "Content-Type: application/json" \
  -d '{"Prompt":"一只小猫","Model":"3.0","GenerateType":"Normal"}'
```

文档：https://cloud.tencent.com/document/api/1804/123447 （Prompt 即文生3D）

---

## 环境变量

| 变量 | 默认 | 说明 |
|------|------|------|
| PORT | 8787 | 监听端口 |
| DATA_DIR | ./data | Cookie 目录 |
| UPSTREAM | https://3d.hunyuan.tencent.com | 网页上游 |
| TENCENT_SECRET_ID | — | 云 API 密钥 ID |
| TENCENT_SECRET_KEY | — | 云 API 密钥 |
| TENCENT_REGION | ap-guangzhou | 云 API 地域 |
| PROXY_QUIET | — | =1 关闭访问日志 |

---

## 相关本地文档

- `D:\lose\docs\api-catalog.md` — 网页端接口目录
- `D:\lose\docs\api-discovery.md` — 逆向过程
- `D:\lose\docs\signature.md` — 网页端签名算法
- 官方云 API 请始终以 https://cloud.tencent.com/document/product/1804 为准

## 纹理（把白模变成带 PBR 的成品）

几何生成的 GLB 是**白模**（`materials=0`）。要拿到带贴图的成品，需再走纹理阶段。

### 本地封装端点（推荐）

```sh
curl -s -X POST http://127.0.0.1:8812/api/text3d/texture \
  -H 'Content-Type: application/json' \
  -d '{"worksId":"<几何任务的 worksId>","method":"image","keepUv":false}'
```

传 `worksId` 即自动推导 `fbxUrl`/`imageUrl`。返回 `worksId`（新任务）供轮询。
`method`：`image`（默认）| `views` | `text`。
`text` 为**文生纹理**（提示词直接生成贴图），会自动选用已 UV 展开的 fbx。
`keepUv` 实测两种取值都能出成品（GLB 均含 `TEXCOORD_0`）。

### 直接调用上游：`image2texture_v31`

```sh
curl -s -X POST http://127.0.0.1:8812/game3d/texture_painting/image2texture_v31 \
  -H 'Content-Type: application/json' \
  -d '{
    "fbxUrl":            "<几何任务的 geometryGenerationRsp.fbxUrl>",
    "imageUrl":          "<几何任务的 geometryGenerationRsp.imageUrl>",
    "dependOnWorksId":   "<几何任务的 worksId>",
    "worksType":         2,
    "requestId":         "go<12位随机>",
    "modelVersion":      "v3.1",
    "keepUv":            false
  }'
```

`worksType` / `requestId` / `modelVersion` 为**必需**，缺任一返回 `200002 输入参数错误`。

完成后 `resultUrls` 出现：

| 字段 | 含义 |
|------|------|
| `texturePaintingRsp.glbUrl` | ★ 带 PBR 贴图的成品模型 |
| `texturePaintingRsp.fbxUrl` | 成品 FBX |
| `texturePaintingRsp.pbrImageUrl` | PBR 基础色 |
| `texturePaintingRsp.pbrNormalImageUrl` | 法线 |
| `texturePaintingRsp.pbrMetallicImageUrl` | 金属度 |
| `texturePaintingRsp.pbrRoughnessImageUrl` | 粗糙度 |

### 验收标准

| 指标 | 白模 | 成品 |
|------|------|------|
| materials / images / textures | 0 / 0 / 0 | 1 / 3 / 3 |
| 大小 | ~0.3 MB | ~16 MB |

**不要只看"有 glbUrl"就判定成功** —— 白模同样有 glbUrl，必须检查 `materials > 0`。

### 文生纹理：用 `texture_edit_local`，**不是** `text2texture_v31`

⚠️ **`/game3d/texture_painting/text2texture_v31` 是死路**：它比其它路径多一步
`hunyuan-3d-texture-preprocess-v1.0`，该步稳定返回
`errorCode: 400 "http client codec StatusCode: Bad Request"`（实测 7/7 全失败）。

**真正的文生纹理入口是 `texture_edit_local` + `editType:1`**（SPA 抓包确认）：

```json
{
  "fbxUrl":          "<已 UV 展开的 fbx，见下方注意事项>",
  "worksId":         "<作品 worksId>",
  "editType":        1,
  "prompt":          "青铜铸造的台灯，做旧金属质感",
  "localNames":      [],
  "segmentationInfo": "",
  "requestId":       "go<随机>"
}
```

它走 `hunyuan-3d-texture-edit-text-v1.0`，**实测成功**（24.2MB 成品，materials=1，贴图 3 张）。

**关键注意事项 —— fbxUrl 必须已 UV 展开**：

| 传入的 fbx | 结果 |
|-----------|------|
| 几何任务的 `geometryGenerationRsp.fbxUrl`（白模，无 UV） | 跑完但**不产出** `texturePaintingRsp` |
| 上一次纹理阶段的 `texturePaintingRsp.fbxUrl`（已 UV） | ✅ 产出完整贴图 |

本地封装端点的 `method="text"` **已自动优先选用已 UV 展开的 fbx**。

`editType` 其它取值（来自 SPA 枚举）：`2`=材质 `3`=图生纹理 `4`=PBR 属性 `5`=多视图。

### 四种纹理路径对照

| 路径 | 输入 | 实测 |
|------|------|------|
| `texture_edit_local` (editType=1) | fbx(UV) + prompt | ✅ **文生纹理** |
| `image2texture_v31` | fbx + 渲染图 | ✅ 5/5 |
| `views2texture` | fbx + 四视图 | ✅ 1/1 |
| `text2texture_v31` | fbx + prompt | ❌ 7/7 失败，**勿用** |

---

