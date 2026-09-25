# Tencent Hunyuan 3D — API Discovery Report

**Host**: `https://3d.hunyuan.tencent.com` (also global `3d.hunyuanglobal.com`)  
**Purpose**: Reverse-proxy in Go  
**Date extracted from**: local Playwright-downloaded JS bundles under `D:\lose\output\playwright\hunyuan\`

---

## 0. Executive summary

| Area | Status | Result |
|------|--------|--------|
| Main-site signature | **SOLVED** | `HmacSHA256(canonicalQS, "Hf6d6KFB3D")` hex, injected as query params `timestamp`/`nonce`/`sign` |
| Concept generation submit | **SOLVED** | Per-pipeline endpoints under `/game3d/*` (concept_design, geometry_generation, texture_painting, motion_generation, …) |
| Concept credit | SOLVED | `POST /game3d/general_info/get_remaining_times` |
| Main credit | SOLVED | `POST /api/3d/quotainfo` |
| Concept axios `Va` | SOLVED | baseURL `""` (same-origin), timeout 600s, envelope `{errNo,errMsg,data}` |
| Login / QR | **PARTIAL** | WeChat official `WxLogin` widget + QQ OAuth + phone/email via prop-injected host APIs. No custom QR poll endpoint found in SDK. Phone/email HTTP paths not hardcoded in downloaded bundles. |
| Cookie / storage names | SOLVED | `hy_user`, `hy_token`, `userInfo`, `hy_device_id`, … |

---

## 1. SIGNATURE algorithm (main site) — module `47436` / `u.Xv`

### 1.1 Where it is used

Only **`POST /api/3d/creations/generations`** sets `signature:!0` in the main SPA API module (`38455`). The axios request interceptor (module `91786`) reads `config.signature`, calls `u.Xv(existingParams, options)`, and writes the result into **URL query params** (not the JSON body).

### 1.2 Exact algorithm

```
inputs:
  e = existing query/body-independent params map (optional)
  t = options (true → {}; object → {nonceLength, timestampField, nonceField, signField})

defaults:
  nonceLength   = 16
  timestampField= "timestamp"
  nonceField    = "nonce"
  signField     = "sign"

steps:
  1. g = {...e}
  2. g[timestampField] = floor(Date.now()/1000)          // unix seconds
  3. g[nonceField]     = random 16 chars from [A-Za-z0-9]
  4. Canonicalize:
       entries = Object.entries(g)
         .filter(([,v]) => v != null && v !== "")
         .map(([k,v]) => [k, String(v)])
         .sort(([a],[b]) => a.localeCompare(b))          // sort by KEY
  5. query = entries.map(([k,v]) => `${k}=${v}`).join("&")
  6. HMAC key = deriveKey(constants)  →  "Hf6d6KFB3D"
  7. g[signField] = CryptoJS.HmacSHA256(query, key).toString(CryptoJS.enc.Hex)
  8. return g   // assign back to axios config.params
```

### 1.3 Key derivation (obfuscated constants)

```
c = Uint8Array([122,59,92,165,30,79,166,139,142,129,139,89,219,131,101,204])
d = Uint8Array([122,59,92,45,30,79,106,139,156,13,46,63,74,91,108,125])
u = [3,5,2,7,1,4,6,2,5,3,1,4,2,6,3,5]          // rotate-left bit counts
m = [14,11,13,9,15,10,12,8,6,3,5,1,7,2,4,0]    // permutation

t[i] = c[i] XOR d[i]
o[i] = rotl8(t[i], u[i])
n[i] = o[m[i]]
key  = utf8(n until first 0x00)

RESULT: key = "Hf6d6KFB3D"
```

Verified by recomputation (see `api-extracts.md`).

### 1.4 Crypto wiring

| Webpack module | Export used | Identity |
|----------------|-------------|----------|
| `3793` | `i` | `CryptoJS.HmacSHA256` |
| `69956` | `a` | `CryptoJS.enc.Hex` |

Present in `utils__98a6d963a73f89a32aed.js` (CryptoJS bundle shared with main SPA).

### 1.5 Go implementation

```go
const hunyuanSignKey = "Hf6d6KFB3D"

func SignHunyuanParams(params map[string]string) map[string]string {
    out := make(map[string]string, len(params)+3)
    for k, v := range params {
        if v != "" && k != "sign" && k != "timestamp" && k != "nonce" {
            out[k] = v
        }
    }
    out["timestamp"] = strconv.FormatInt(time.Now().Unix(), 10)
    out["nonce"] = randomAlnum(16)

    keys := make([]string, 0, len(out))
    for k := range out { keys = append(keys, k) }
    sort.Strings(keys)

    var sb strings.Builder
    for i, k := range keys {
        if i > 0 { sb.WriteByte('&') }
        sb.WriteString(k); sb.WriteByte('='); sb.WriteString(out[k])
    }

    mac := hmac.New(sha256.New, []byte(hunyuanSignKey))
    mac.Write([]byte(sb.String()))
    out["sign"] = hex.EncodeToString(mac.Sum(nil))
    return out
}
```

**Critical notes for Go proxy:**
- Signature fields go on the **query string**, even for POST.
- Body is separate JSON (generation payload).
- Values are stringified before sort/join; null/empty omitted.
- Key sort uses JS `localeCompare` — for ASCII API keys this equals byte sort.
- Nonce charset: `ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789`.

---

## 2. Main-site HTTP client (module `91786`)

```js
axios.create({
  baseURL: "/",
  withCredentials: true,
  headers: {
    "Content-Type": "application/json",
    "x-source": "web",
    "x-product": "hunyuan3d"
  },
  timeout: 120000
})
```

### Request interceptors (order)

1. Inject `x_hunyuan_inner_user_id` from storage/cookie `hy_user`
2. `Md` → add `Trace-Id` = UUID without dashes; also `startTime`, `fromUrl`
3. Signature interceptor (if `config.signature`)
4. Cache interceptor (if `config.useCache`, default TTL 9e5 ms)

### Response interceptor

- HTTP 401 → clear session, redirect `/login?redirect_url=<current href>`
- Business error message from `data.message` / `data.error.message`

### Storage / cookie names (module `86147`)

| Constant | Name | Role |
|----------|------|------|
| `Iq` | **`hy_user`** | user id; also `x_hunyuan_inner_user_id` header |
| `iS` | **`hy_token`** | auth token |
| `gm` | `userInfo` | cached user object |
| `V2` | `hy_global_config` | remote config cache |
| `HV` | `hy_device_id` | device id |
| `OG` | `__BEACON_deviceId` | telemetry device id |
| `qK` | `user_login_emails` | email login cache |
| `l1` | `hideBulletinDialog_ploy_gen` | UI flag |

> Server auth primarily uses **HTTP cookies** (`withCredentials:true`). `hy_user` / `hy_token` are read client-side (js-cookie-like getter `s.A.get`) and may be cookies set at login. Exact cookie domain/path not visible in JS; capture from browser DevTools `document.cookie` / Set-Cookie on login.

### Additional headers observed

- `x_hunyuan_inner_user_id` (optional)
- `Trace-Id` (always via Md)
- `weappHeaders` from `sessionStorage` on COS upload (fileName upload flow)

---

## 3. Main-site endpoint catalog

Base: `https://3d.hunyuan.tencent.com/` (prefix `n.m` = `""`)

| Method | Path | Signature | Notes |
|--------|------|-----------|-------|
| GET | `/api/3d/config` | no | `useCache:true` |
| POST | `/api/3d/resource/genUploadInfo` | no | COS upload info; body `{fileName}` + optional weappHeaders |
| POST | `/api/3d/creations/generations` | **YES** | main generation submit; headers spread |
| GET | `/api/3d/creations/detail?creationsId=` | no | deduped via wrapper `56146` |
| POST | `/api/3d/resource/review` | no | content review |
| POST | `/api/3d/resource/cmykCheck` | no | image CMYK check |
| POST | `/api/3d/creations/boneBindingFromMesh` | no | |
| POST | `/api/3d/creations/manualBoneBindingFromMesh` | no | |
| POST | `/api/3d/creations/manualBoneBinding` | no | |
| POST | `/api/3d/creations/cancel` | no | |
| POST | `/api/3d/creations/count` | no | progress count |
| POST | `/api/3d/creations/retopologize` | no | |
| POST | `/api/3d/creations/delete` | no | |
| POST | `/api/3d/creations/resourceConvert` | no | → `.then(e=>e.data)` |
| POST | `/api/3d/quotainfo` | no | **main credit** |
| GET | `/api/3d/planCreditInfo` | no | |
| GET | `/api/3d/purchaseList` | no | |
| POST | `/api/3d/purchaseBasicPlan` | no | |
| POST | `/api/3d/feedback/submit` | no | |
| POST | `/api/3d/notice/list` | no | |
| POST | `/api/3d/notice/statusUpdate` | no | |
| POST | `/api/3d/worldPlay/text2Image` | no | |
| POST | `/api/3d/worldPlay/page2video` | no | `responseType:"blob"` |
| POST | `/api/3d/worldScene/review` | no | |
| GET | `/api/3d/interaction/config` | no | |
| GET | `/api/3d/worldPlay/config` | no | |
| GET | `/api/open/auth?appID=&code=&signature=` | no* | OAuth code exchange (*`signature` is URL field from IdP) |
| GET | `/api/profile` | no | path string present in bundle (user profile) |

### Generation request/response notes

- Generation POST body shape is UI-driven (text/image/multi-view/animation/etc.); not fully typed in minified bundle.
- Signature params are merged into query: `timestamp`, `nonce`, `sign`.
- Upload flow: `genUploadInfo` → COS `putObject` with tmp credentials from response (`encryptTmpSecretId/Key/Token`, `bucketName`, `region`, `location`, `resourceUrl`).

### Quota response shape (from React context default)

```js
quotaInfo: {
  date: "",
  totalQuota: 0,
  alarmQuota: 0,
  remainQuota: 0,
  consumeQuota: 0,
  showUserInviteQuotaTag: false,
  perUserInviteQuotaCount: 0,
  maxUserInviteQuota: 0,
  userInviteQuota: 0
}
```

---

## 4. CONCEPT studio (`/studio/creation/concept`)

### 4.1 Axios client `Va`

| Setting | Value |
|---------|-------|
| `baseURL` | `""` (same-origin relative) |
| `timeout` | `600000` (10 min) |
| `withCredentials` | not set (same-origin cookies still sent) |
| Extra headers | **none** (`x-source` / `x-product` **not** applied on concept client) |
| Success envelope | `{errNo: 0, errMsg, data}` |
| Failure | `errNo !== 0` → throw `errMsg` |
| 401 | redirect `/login?redirect_url=...` |

Production hosts include the main host: `3d.hunyuan.tencent.com`, `3d.hunyuanglobal.com`. Internal hosts: `*.hunyuan.woa.com`, `3d.pre.hunyuan.woa.com`.

### 4.2 Concept generation submit APIs (the missing piece)

Generation is **not** `/api/3d/creations/generations`. Concept studio submits per-pipeline tasks that return `{worksId}`:

#### Concept design
| Method | Path | Body (extracted) |
|--------|------|------------------|
| POST | `/game3d/concept_design/text2image` | `{prompt, style?, apose?}` |
| POST | `/game3d/concept_design/image2views` | `{imageUrl, style?, apose?}` |

Styles: `""`, `q_version`, `steampunk`, `futuristic`, `pixel`, `hand_drawn`, `low-poly`

#### Geometry generation
| Method | Path | Body |
|--------|------|------|
| POST | `/game3d/geometry_generation/text2geometry` | `{prompt, modelVersion, apose?, faceCount}` |
| POST | `/game3d/geometry_generation/image2geometry` | `{singleImage, modelVersion, dependOnWorksId?, faceCount?, boundingBoxList?, apose?}` |
| POST | `/game3d/geometry_generation/image2geometry_singleimage` | EN single-image variant |
| POST | `/game3d/geometry_generation/image2geometry_views` | EN multi-view variant |
| POST | `/game3d/geometry_generation/views2geometry` | `{modelVersion, viewsImage, faceCount}` |
| POST | `/game3d/geometry_generation/5views2geometry` | `{singleImage, modelVersion, dependOnWorksId, viewsImage, faceCount}` |

`modelVersion`: `v3.0` | `v3.1` | `v3.5-preview`

#### Texture painting
| Method | Path |
|--------|------|
| POST | `/game3d/texture_painting/image2texture_v31` |
| POST | `/game3d/texture_painting/image2texture_v35preview` |
| POST | `/game3d/texture_painting/views2texture` |
| POST | `/game3d/texture_painting/views2texture_v35preview` |
| POST | `/game3d/texture_painting/text2texture` (v3.5) |
| POST | `/game3d/texture_painting/text2texture_v31` |
| POST | `/game3d/texture_painting/texture_segmentation` |
| POST | `/game3d/texture_painting/texture_edit_local` |
| POST | `/game3d/texture_painting/update_texture_painting` |

#### Motion
| Method | Path | Body |
|--------|------|------|
| POST | `/game3d/motion_generation/text2motion` | `{textPrompt, duration, disableRewrite, dependOnWorksId?, retargetFbx}` |
| POST | `/game3d/motion_generation/video2motion` | (video-driven) |
| POST | `/game3d/motion_retarget/motion_retarget` | `{motionType, worksId\|dependOnWorksId, fbxUrl, imageUrl?}` |

#### Other pipelines
| Method | Path |
|--------|------|
| POST | `/game3d/bone_skinning/bone_skinning` |
| POST | `/game3d/uv_unwrapping/uv_unwrapping` |
| POST | `/game3d/low_poly_topology/low_poly_topology` |
| POST | `/game3d/low_poly_topology/image2low_poly` |

#### Component splitting
| Method | Path |
|--------|------|
| POST | `/game3d/component_splitting/v2/pre_segmentation` | mode `full_segmentation` \| `interactive_segmentation` |
| POST | `/game3d/component_splitting/v2/part_split` | + `userId` |
| POST | `/game3d/component_splitting/v2/smooth_segmentation` | |
| POST | `/game3d/component_splitting/v1_5/part_split` | |
| POST | `/game3d/component_splitting/part_split` | |
| POST | `/game3d/component_splitting/part_merge` | |
| POST | `/game3d/component_splitting/part_adjust` | |
| POST | `/game3d/component_splitting/update_component_splitting` | |

### 4.3 Concept works / credit / resource / share / whitelist

| Method | Path | Notes |
|--------|------|-------|
| POST | `/game3d/general_info/get_remaining_times` | **concept credit** |
| POST | `/game3d/general_info/get_works_list` | `{...filters, worksPipeline?, pipelineStatus?}` |
| POST | `/game3d/general_info/delete_works` | |
| POST | `/game3d/general_info/regenerate_work` | `{worksId}` → `{data:{worksId}}` |
| POST | `/game3d/general_info/get_good_case` | |
| POST | `/game3d/general_info/get_motion_case` | |
| POST | `/game3d/resource/upload` | |
| POST | `/game3d/resource/download` | blob |
| POST | `/game3d/resource/character_validations` | |
| POST | `/game3d/resource/format_conversions` | `{fbxUrl, fmt}` → `{data:{rspUrl}}` |
| POST | `/game3d/share/create` | `{worksId, modelUrl}` |
| GET | `/game3d/share/detail?shareToken=` | |
| POST | `/game3d/feedback/like\|dislike\|get\|get_dislike_reasons\|cancel` | |
| GET | `/3d/getuserinfo` | concept user info |
| POST | `/new-portal/whitelist/query` | `{business:"3dgame", scene:"all"}` |
| POST | `/new-portal/whitelist/apply` | `{business, scene, message}` |

### 4.4 Works list / pipeline enums (partial)

```
pipelineStatus: SUCCESS=2, FAILED=3, CANCEL=4, DELETED=5  (+ running states)
worksPipeline includes: CONCEPT, GEOMETRY, COMPONENT, POLY, TEXTURE,
  RIGGING_AND_SKINNING, ANIMATION_EFFECT, ...
component operationType: PRE_PART=1, BRUSH_ADJUST=2, GENERATE_PART=3,
  INTERACTIVE_SEGMENTATION=4, SMOOTH_SEGMENTATION=5, RE_SEGMENTATION=6,
  BRUSH_ADJUST_PAINT=7, BRUSH_ADJUST_SPLIT_MERGE=8
```

---

## 5. LOGIN / QR flow

### 5.1 What the tencent-sdk hardcodes

| Endpoint / URL | Role |
|----------------|------|
| `GET /api/jointappid` | fetch WeChat/QQ joint `appId` (default prop `getAppId`) |
| `GET /api/auth-center/oauth2/authorize` | Taihu partner OAuth (`https://tai.it.tencent.com`) |
| `https://graph.qq.com/oauth2.0/authorize` | QQ OAuth (client_id + redirect_uri + state + nonce) |
| `//res.wx.qq.com/connect/zh_CN/htmledition/js/wxLogin.js` | WeChat official QR widget SDK |
| `//captcha.gtimg.com/TCaptcha.js` | Tencent captcha for SMS/email code |

### 5.2 QR login flow (WeChat)

There is **no custom Hunyuan QR poll API** in the SDK. Flow is the official WeChat open-platform scan widget:

1. Host calls `GET /api/jointappid` → `appId`
2. Page mounts `new WxLogin({appid, scope:"snsapi_login", redirect_uri, state:"wechat_login"| "wechat_bind,<id>", ...})`
3. User scans QR in WeChat iframe
4. WeChat redirects browser to `redirect_uri` with `?code=...&state=wechat_login` (+ client `nonce` appended by app)
5. Host backend / SPA exchanges code via **`GET /api/open/auth?appID=&code=&signature=`**
6. Server sets session cookies; SPA stores `hy_user` / `hy_token` / `userInfo`

**Implication for Go proxy:** QR login requires either (a) full OAuth redirect handling with a public callback URL, or (b) replaying browser cookies after manual login. There is no lightweight poll endpoint to abuse.

### 5.3 QQ login flow

```
https://graph.qq.com/oauth2.0/authorize
  ?response_type=code
  &client_id={appId}
  &redirect_uri={redirect}{&|?}nonce={nonce}&state={qq_login|qq_bind,bindId}&isMobile={0|1}
  &display=
```
Then same host callback → `/api/open/auth`.

### 5.4 Phone / email login

SDK builds payloads but **does not hardcode HTTP paths**. Host login page (lazy chunk not in current downloads) injects:

```js
sendVerificationCode({ phoneNumber|email, captchaTicket, captchaRandstr })
confirm({ phoneNumber|email, verificationCode, appId? })
```

Likely paths live on the host login SPA (search `/login` page chunk for `/api/auth` / `/api/login` / `/api/sms`). **Not fully extractable from current downloads.**

Phone confirm payload: `{phoneNumber, verificationCode, appId}`  
Email confirm payload: `{email, verificationCode}`

### 5.5 Login localStorage keys (SDK side)

```
hyc-login-nonce
hyc-login-target-url
hyc-login-last-type        // phone | wechat | qq | email
hyc-login-fail-target-url
hyc-login-extra-payload
hyc-login-app-id
```

### 5.6 Auth cookie / storage names (main + concept)

| Name | Where | Purpose |
|------|-------|---------|
| `hy_user` | cookie/localStorage (module 86147 `Iq`) | user id, header `x_hunyuan_inner_user_id` |
| `hy_token` | cookie/localStorage (`iS`) | bearer/session token |
| `userInfo` | storage (`gm`) | cached profile |
| `hy_device_id` | storage (`HV`) | device |
| `__BEACON_deviceId` | storage (`OG`) | telemetry |
| `hy_global_config` | storage (`V2`) | config |

Server Set-Cookie names after login are **not named in JS**; they are opaque session cookies sent with `withCredentials:true`. For a Go reverse-proxy, forward the full `Cookie` header from a captured browser session.

---

## 6. Environment / CDN hosts

| Purpose | Host |
|---------|------|
| Main / concept prod | `3d.hunyuan.tencent.com`, `3d.hunyuanglobal.com` |
| Internal prod | `hunyuan-3d.woa.com` |
| Pre | `3d.pre.hunyuan.woa.com`, `hunyuan-3d-pre.woa.com` |
| Test | `hunyuan-3d-test.woa.com`, `game-testin.hunyuan.woa.com` |
| Concept chunks CDN | `https://cdn-game-3d.qstatic.com/game3d/assets/<name>` |
| COS buckets (upload) | `hunyuan-base-prod-1258344703.cos.ap-guangzhou.myqcloud.com` (+ accelerate) |
| Example asset COS | `tts-1258344706.cos.ap-guangzhou.myqcloud.com` |

Concept production detection:
```
window.location.hostname includes test|lab|dev → "dev"
else if in PRE lists → "pre"
else if in PROD lists → "online"
```

---

## 7. Go reverse-proxy checklist

### Must implement

1. **Sign** `POST /api/3d/creations/generations` query params:
   - `timestamp` = unix seconds
   - `nonce` = 16 alnum
   - `sign` = `hex(HmacSHA256(sortedK=V, "Hf6d6KFB3D"))`
2. Always send main-site headers:
   - `x-source: web`
   - `x-product: hunyuan3d`
   - `Content-Type: application/json`
   - `Trace-Id: <uuid-no-dashes>`
   - optional `x_hunyuan_inner_user_id`
3. Forward cookies; `withCredentials` equivalent.
4. Concept endpoints: same host, relative `/game3d/...`, **no** x-source/x-product required by bundle; cookies still needed; treat `errNo==0` as success.
5. Credit endpoints:
   - Main: `POST /api/3d/quotainfo`
   - Concept: `POST /game3d/general_info/get_remaining_times`

### Generation submit routing

| Feature | Endpoint family |
|---------|-----------------|
| Main site 3D gen | `POST /api/3d/creations/generations` **+ signature** |
| Concept text/image design | `/game3d/concept_design/*` |
| Concept mesh from text/image/views | `/game3d/geometry_generation/*` |
| Texture | `/game3d/texture_painting/*` |
| Animation | `/game3d/motion_generation/*`, `/game3d/motion_retarget/*` |
| Rigging | `/game3d/bone_skinning/bone_skinning` |
| Topology | `/game3d/low_poly_topology/*` |
| UV | `/game3d/uv_unwrapping/uv_unwrapping` |
| Parts / segmentation | `/game3d/component_splitting/*` |

### Login for automation

- Prefer capturing a real browser session (cookies `hy_*` + server session) via Playwright login once, then replay Cookie header in Go.
- QR = WeChat official widget; no first-party poll API to call from Go alone.
- Phone/email endpoints remain **unknown pathnames** — next step: download the `/login` route chunk from the main SPA (search lazy `import("./index-*.js")` near route `LOGIN` / `login-email`).

### Chunks worth downloading next

From main SPA concept/studio lazy map (CDN `https://cdn-game-3d.qstatic.com/game3d/assets/`):
- Login route chunk (not yet identified in `index__aa01d2040ee35d2c8c78.js` lazy imports — main SPA is a thin shell; login page may be a separate entry).
- `assets/index-FvUMGtrK.js` (already saved as concept_index.js duplicate)
- Any `api-*.js` from **main** site (not concept) if present in network tab.

Also check browser network for:
- `Set-Cookie` names on `/login` success
- phone/email request paths when using SMS login
- sample `POST /api/3d/creations/generations` query (`timestamp&nonce&sign`) vs body

---

## 8. Source map (files → findings)

| File | Findings |
|------|----------|
| `index__aa01d2040ee35d2c8c78.js` | axios 91786, signature interceptor, API module 38455, storage 86147, Trace-Id Md |
| `utils__98a6d963a73f89a32aed.js` | module 3793=HmacSHA256, 69956=enc.Hex (crypto-js) |
| `concept_index.js` / `assets/index-FvUMGtrK.js` | Va client, all base `/game3d` wrappers, host lists |
| `concept_chunks/index-CdHjtkz5.js` | concept_design text2image/image2views + payloads |
| `concept_chunks/GeometryLeftPanel-BEIfkEln.js` | geometry_generation endpoints + payloads |
| `concept_chunks/utils-Duo5gWek.js` | texture_painting endpoints |
| `concept_chunks/index-DYOT9BMy.js` | motion_generation / motion_retarget |
| `concept_chunks/index-C7l2QcrV.js` | bone_skinning |
| `concept_chunks/index-CkOC3f35.js` | uv_unwrapping |
| `concept_chunks/index-D1l1ipRx.js` | low_poly_topology |
| `concept_chunks/index-D0dNGWFS.js` | component_splitting call sites |
| `tencent-sdk__3a40fc4c813975ab667b.js` | login SDK: WxLogin, QQ OAuth, jointappid, auth-center, phone/email props |

---

## 9. Known gaps / follow-ups

1. **Phone/email login HTTP paths** — not in tencent-sdk; need main `/login` page chunk or live network capture.
2. **Server Set-Cookie names** — not in JS; capture from live login response headers.
3. **Main-site generations body schema** — UI-driven; capture one real POST body via DevTools.
4. **Concept works polling** — task status likely via `get_works_list` filter on `pipelineStatus`; exact poll interval not extracted.
5. **`api-fBWiQkV4.js`** — 683 bytes; it is a **video overlay toast helper**, NOT the API layer. Real concept APIs are inlined in `concept_index.js` + lazy route chunks (downloaded).

Raw function snippets: `D:\lose\docs\api-extracts.md`
