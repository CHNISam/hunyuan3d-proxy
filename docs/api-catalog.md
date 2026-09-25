# 腾讯混元3D 网页端 API 目录

站点：`https://3d.hunyuan.tencent.com/`

两套前端：
1. **主站**（webpack SPA）：文生/图生3D、纹理、动画、工作流等
2. **Concept 工作室**：`/studio/creation/concept`（Vite SPA，`cdn-game-3d.qstatic.com`）

## 通用请求头（主站）

```
Content-Type: application/json
x-source: web
x-product: hunyuan3d
```

Cookie 会话；本地键：
- `hy_user`（cookie 名 / storage key `Iq`）
- `hy_token`
- `userInfo`
- `hy_device_id`
- 请求头 `x_hunyuan_inner_user_id` ← `hy_user`

401 → 跳转 `/login?redirect_url=...`

---

## 主站 API（baseURL = `/`）

### 积分（独立，与 concept 不同）

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/3d/quotainfo` | 主站配额/积分查询 |
| GET  | `/api/3d/planCreditInfo` | 套餐积分信息 |
| GET  | `/api/3d/purchaseList` | 购买列表 |
| POST | `/api/3d/purchaseBasicPlan` | 购买基础套餐 |

### 生成与作品

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/3d/creations/generations` | 提交生成任务（**需签名**） |
| GET  | `/api/3d/creations/detail?creationsId=` | 任务详情 |
| POST | `/api/3d/creations/count` | 作品计数/列表辅助 |
| POST | `/api/3d/creations/delete` | 删除 |
| POST | `/api/3d/creations/cancel` | 取消 |
| POST | `/api/3d/creations/retopologize` | 重拓扑 |
| POST | `/api/3d/creations/resourceConvert` | 格式转换 |
| POST | `/api/3d/creations/boneBindingFromMesh` | 骨骼绑定 |
| POST | `/api/3d/creations/manualBoneBinding` | 手动绑骨 |
| POST | `/api/3d/creations/manualBoneBindingFromMesh` | 网格手动绑骨 |

### 资源

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/3d/resource/genUploadInfo` | 获取上传凭证 |
| POST | `/api/3d/resource/review` | 资源审核 |
| POST | `/api/3d/resource/cmykCheck` | CMYK 检查 |

### 其它

| 方法 | 路径 | 说明 |
|------|------|------|
| GET  | `/api/3d/config` | 全局配置 |
| POST | `/api/3d/feedback/submit` | 反馈 |
| GET  | `/api/3d/interaction/config` | 交互配置 |
| GET  | `/api/3d/worldPlay/config` | 玩法配置 |
| POST | `/api/3d/worldPlay/text2Image` | 文生图 |
| POST | `/api/3d/worldPlay/page2video` | 页面转视频 |
| POST | `/api/3d/worldScene/review` | 世界场景审核 |
| POST | `/api/3d/notice/list` | 通知列表 |
| POST | `/api/3d/notice/statusUpdate` | 通知状态 |
| GET  | `/api/open/auth?appID=&code=&signature=` | 开放鉴权 |

### 登录相关

| 方法 | 路径 | 说明 |
|------|------|------|
| GET  | `/api/jointappid` | 获取联合登录 appId（微信/QQ） |
| GET  | `/api/auth-center/oauth2/authorize` | 合作伙伴 OAuth |
| GET  | `/api/open/auth` | code 换会话 |

微信扫码：`WxLogin` 组件，`scope=snsapi_login`，redirect 带 nonce。  
QQ：`https://graph.qq.com/oauth2.0/authorize?...`。

---

## Concept 工作室 API（同 host）

### 积分（独立）

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/game3d/general_info/get_remaining_times` | **Concept 剩余次数** |
| GET  | `/3d/getuserinfo` | Concept 用户信息 |

### 作品管理

| 方法 | 路径 |
|------|------|
| POST | `/game3d/general_info/get_works_list` |
| POST | `/game3d/general_info/delete_works` |
| POST | `/game3d/general_info/regenerate_work` |
| POST | `/game3d/general_info/get_good_case` |
| POST | `/game3d/general_info/get_motion_case` |

### 生成（输入输出核心）

| 方法 | 路径 | 功能 |
|------|------|------|
| POST | `/game3d/concept_design/text2image` | 文生概念图 |
| POST | `/game3d/concept_design/image2views` | 图生多视图 |
| POST | `/game3d/geometry_generation/text2geometry` | 文生几何 |
| POST | `/game3d/geometry_generation/image2geometry` | 图生几何 |
| POST | `/game3d/geometry_generation/image2geometry_singleimage` | 单图几何 |
| POST | `/game3d/geometry_generation/image2geometry_views` | 多视图几何 |
| POST | `/game3d/geometry_generation/views2geometry` | 多视图→几何 |
| POST | `/game3d/geometry_generation/5views2geometry` | 五视图→几何 |
| POST | `/game3d/component_splitting/v2/pre_segmentation` | 预分割 |
| POST | `/game3d/component_splitting/v2/part_split` | 部件分割 |
| POST | `/game3d/component_splitting/v2/smooth_segmentation` | 平滑分割 |
| POST | `/game3d/component_splitting/part_split` | 分割 v1 |
| POST | `/game3d/component_splitting/part_merge` | 部件合并 |
| POST | `/game3d/component_splitting/part_adjust` | 部件调整 |
| POST | `/game3d/component_splitting/update_component_splitting` | 更新分割 |
| POST | `/game3d/low_poly_topology/low_poly_topology` | 低模拓扑 |
| POST | `/game3d/low_poly_topology/image2low_poly` | 图生低模 |
| POST | `/game3d/uv_unwrapping/uv_unwrapping` | 语义 UV |
| POST | `/game3d/texture_painting/text2texture` | 文生纹理 |
| POST | `/game3d/texture_painting/text2texture_v31` | 文生纹理 v3.1 |
| POST | `/game3d/texture_painting/image2texture_v31` | 图生纹理 v3.1 |
| POST | `/game3d/texture_painting/image2texture_v35preview` | 图生纹理 v3.5 |
| POST | `/game3d/texture_painting/views2texture` | 多视图纹理 |
| POST | `/game3d/texture_painting/views2texture_v35preview` | 多视图纹理 v3.5 |
| POST | `/game3d/texture_painting/texture_edit_local` | 局部纹理编辑 |
| POST | `/game3d/texture_painting/texture_segmentation` | 纹理分割 |
| POST | `/game3d/texture_painting/update_texture_painting` | 更新纹理 |
| POST | `/game3d/bone_skinning/bone_skinning` | 骨骼蒙皮 |
| POST | `/game3d/motion_generation/text2motion` | 文生动作 |
| POST | `/game3d/motion_generation/video2motion` | 视频生动作 |
| POST | `/game3d/motion_retarget/motion_retarget` | 动作重定向 |

### 资源 / 分享 / 反馈

| 方法 | 路径 |
|------|------|
| POST | `/game3d/resource/upload` |
| POST | `/game3d/resource/download` |
| POST | `/game3d/resource/character_validations` |
| POST | `/game3d/resource/format_conversions` |
| POST | `/game3d/share/create` |
| GET  | `/game3d/share/detail?shareToken=` |
| POST | `/game3d/feedback/like` |
| POST | `/game3d/feedback/dislike` |
| POST | `/game3d/feedback/cancel` |
| POST | `/game3d/feedback/get` |
| POST | `/game3d/feedback/get_dislike_reasons` |
| POST | `/new-portal/whitelist/apply` |
| POST | `/new-portal/whitelist/query` |

---

## 双端点模型（反代设计）

| 维度 | 主站 main | Concept |
|------|-----------|---------|
| 页面入口 | `https://3d.hunyuan.tencent.com/` | `https://3d.hunyuan.tencent.com/studio/creation/concept` |
| API base | 同 host `/api/3d/*` | 同 host `/game3d/*`、`/3d/*` |
| 积分查询 | `quotainfo` + `planCreditInfo` | `get_remaining_times`（**独立**） |
| 用户信息 | 主站 profile/cookie | `GET /3d/getuserinfo` |
| 生成入口 | `creations/generations`（签名） | `concept_design/*`、`geometry_generation/*` 等 |

反代应支持：
- 模式切换：`X-Hy-Mode: main|concept` 或路由前缀
- 积分接口分开暴露
- 统一 Cookie 会话，按上游路径选择正确 Referer
