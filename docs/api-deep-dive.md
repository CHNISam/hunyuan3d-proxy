# Tencent Hunyuan 3D — API Deep Dive (from JS bundles)

> Source bundles
> - `D:/lose/output/playwright/hunyuan/index__aa01d2040ee35d2c8c78.js` (main SPA webpack)
> - `D:/lose/output/playwright/hunyuan/concept_index.js` (concept / pipeline SPA, ~10 MB)
> - `D:/lose/output/playwright/hunyuan/concept_chunks/*` (pipeline feature chunks)
> - `D:/lose/output/playwright/hunyuan/tencent-sdk__3a40fc4c813975ab667b.js` (login / beacon SDK)
> - `D:/lose/output/playwright/hunyuan/utils__98a6d963a73f89a32aed.js` (crypto-js HmacSHA256)
>
> Module extracts:
> - `D:/lose/docs/module-91786.txt` — axios instance
> - `D:/lose/docs/module-47436.txt` — signature function `Xv`

---

## 0. Two HTTP clients (critical for a Go reverse proxy)

| Surface | Client | baseURL | Endpoint prefix | Auth |
|---|---|---|---|---|
| **Main SPA** (legacy text/image→3D, worldPlay) | module **91786** `g` (axios) | `"/"` | `/api/3d/...`, `/api/open/auth` | cookies + optional HMAC query signature |
| **Concept / pipeline SPA** (works, geometry, texture, motion…) | class **Va** in `concept_index.js` | `"/api"` | `/game3d/...`, `/3d/getuserinfo`, `/new-portal/...` | cookies (`withCredentials` implied by same-origin axios defaults when configured; Va itself does not set `withCredentials` explicitly — same-origin browser cookies still go) |

Effective absolute paths used by concept client:

```
POST /api/game3d/general_info/get_works_list
POST /api/game3d/geometry_generation/image2geometry
...
GET  /api/3d/getuserinfo
```

Main SPA paths are already absolute under `/api/3d/...`.

---

## 1. Module 91786 — axios instance (Main SPA)

Full source: `D:/lose/docs/module-91786.txt`

```js
91786:(e,t,o)=>{
  o.d(t,{m:()=>p, g:()=>h});
  // imports: axios=a.A, cache=WQ, env=_$, cookie=18987, keys=86147, sign=47436
  const p = ""; // path prefix (empty)
  const g = axios.create({
    baseURL: "/",
    withCredentials: true,
    headers: {
      "Content-Type": "application/json",
      "x-source": "web",
      "x-product": "hunyuan3d",
    },
    timeout: 12e4, // 120_000 ms
  });

  const h = cacheAdapter(g, {
    methods: ["get"],
    ttl: 18e5, // 30 min
    storage: online ? localStorage("axios-cache:") : undefined,
    cacheTakeover: false,
    interpretHeader: false,
    generateKey: (cfg) => {
      const userId = cookie.get(keys.Iq); // "hy_user"
      const url = normalizeURL(cfg);
      return userId ? `${hash(userId)}${url}` : url;
    },
  });

  // Request interceptors (in order)
  // 1) inject inner user id header from cookie hy_user
  h.interceptors.request.use((cfg) => {
    const uid = cookie.get("hy_user");
    if (uid && cfg.headers && !cfg.headers.x_hunyuan_inner_user_id)
      cfg.headers.x_hunyuan_inner_user_id = uid;
    return cfg;
  });
  // 2) Trace-Id + startTime + fromUrl  (module 6306 Md)
  h.interceptors.request.use(traceRequest);
  // 3) HMAC signature when config.signature is true/object
  h.interceptors.request.use((cfg) => {
    const { signature, ...rest } = cfg;
    if (!signature) return cfg;
    const params = {};
    if (rest.params) Object.assign(params, rest.params);
    try {
      const opts = typeof signature === "object" ? signature : {};
      const signed = Xv(params, opts); // module 47436
      return { ...rest, params: signed };
    } catch (e) {
      console.log("SignatureRequestInterceptor error", e);
      return cfg;
    }
  });
  // 4) dev-only extra interceptor
  // 5) useCache flag → axios-cache
  h.interceptors.request.use((cfg) => {
    const { useCache, ...rest } = cfg;
    let cache = false;
    if (useCache) cache = typeof useCache === "object" ? useCache : { ttl: 9e5 };
    return { ...rest, cache };
  });

  // Response
  h.interceptors.response.use(null, (err) => {
    const res = err.response;
    if (res && res.status === 401 && !isInternal()) {
      if (window.location.pathname !== "/login")
        window.location.replace(`/login?redirect_url=${encodeURIComponent(window.location.href)}`);
      return Promise.reject({ ...err, message: "身份验证已过期, 请重新登录" });
    }
    const msg = res?.data?.message || res?.data?.error?.message;
    if (msg) err.message = msg;
    return Promise.reject(err);
  });
  h.interceptors.response.use(null, reportErrorInterceptor); // 6306.in
}
```

### Headers a Go proxy must preserve / inject

| Header | Value | Source |
|---|---|---|
| `Content-Type` | `application/json` | axios default |
| `x-source` | `web` | static |
| `x-product` | `hunyuan3d` | static |
| `x_hunyuan_inner_user_id` | cookie `hy_user` value | interceptor |
| `Trace-Id` | UUID without dashes | interceptor 6306 |
| Cookie | at least `hy_user`, `hy_token` | browser |

### Module 6306 request interceptor (Trace-Id)

```js
Md = (cfg) => {
  const startTime = Date.now();
  const traceId = uuid().replace(/-/g, "");
  return {
    ...cfg,
    headers: { ...cfg.headers, "Trace-Id": traceId },
    startTime,
    fromUrl: location.href,
  };
};
```

### Cookie / storage key constants (module 86147)

```js
Iq = "hy_user"                         // user id cookie; also x_hunyuan_inner_user_id
iS = "hy_token"                        // auth token cookie
gm = "userInfo"                        // localStorage user object
V2 = "hy_global_config"                // localStorage
HV = "hy_device_id"                    // localStorage device id
OG = "__BEACON_deviceId"               // localStorage beacon device id
qK = "user_login_emails"               // localStorage
l1 = "hideBulletinDialog_ploy_gen"     // localStorage
```

Logout clears: cookie `hy_user`, cookie `hy_token`, localStorage/sessionStorage `userInfo`.

---

## 2. Module 47436 — HMAC signature (`Xv`)

Full source: `D:/lose/docs/module-47436.txt`

Exports:
- `Xv(params, options)` → signed params object
- `mI()` → `Math.floor(Date.now()/1000)`
- `n3(len=16)` → random alphanumeric nonce (A–Z a–z 0–9)

### Options (defaults)

```js
{
  nonceLength: 16,
  timestampField: "timestamp",
  nonceField: "nonce",
  signField: "sign",
}
```

### Algorithm (Go-portable)

1. Copy request params (query params for `signature:!0` calls — the interceptor merges `cfg.params`, not JSON body).
2. `params[timestampField] = unix_seconds`.
3. `params[nonceField] = random_alnum(nonceLength)`.
4. Drop entries where value is `null` or `""`.
5. Convert every remaining value with `String(value)`.
6. Sort entries by key using `String.localeCompare` (JS default locale; for ASCII keys this is code-point order).
7. Canonical string: `key1=val1&key2=val2&...`.
8. Derive HMAC key (obfuscated constants, decoded at runtime):

```
c = [122,59,92,165,30,79,166,139,142,129,139,89,219,131,101,204]
d = [122,59,92, 45,30,79,106,139,156, 13, 46, 63, 74, 91,108,125]
rot = [3,5,2,7,1,4,6,2,5,3,1,4,2,6,3,5]
perm = [14,11,13,9,15,10,12,8,6,3,5,1,7,2,4,0]

t[i] = c[i] XOR d[i]
o[i] = rotl8(t[i], rot[i])
n[i] = o[perm[i]]
secret = utf8_decode(n up to first 0x00, else 16 bytes)
```

**Decoded secret: `Hf6d6KFB3D`** (10 chars; bytes `[72,102,54,100,54,75,70,66,51,68,0,...]`)

9. `sign = Base64( HMAC_SHA256(canonical, secret) )`  
   CryptoJS: `HmacSHA256(canonical, secret).toString(enc.Base64)`  
   Confirmed: utils webpack module **3793** = `crypto-js.HmacSHA256`; module **69956** = Base64 encoder.

10. Return `params` with `sign` set to that Base64 string.

### Reference Go sketch

```go
func SignHunyuan(params map[string]string) map[string]string {
    params["timestamp"] = strconv.FormatInt(time.Now().Unix(), 10)
    params["nonce"] = randomAlnum(16) // A-Za-z0-9

    keys := make([]string, 0, len(params))
    for k, v := range params {
        if v == "" { continue }
        keys = append(keys, k)
    }
    sort.Strings(keys) // ASCII / localeCompare for these keys

    var b strings.Builder
    for i, k := range keys {
        if i > 0 { b.WriteByte('&') }
        b.WriteString(k); b.WriteByte('='); b.WriteString(params[k])
    }
    mac := hmac.New(sha256.New, []byte("Hf6d6KFB3D"))
    mac.Write([]byte(b.String()))
    params["sign"] = base64.StdEncoding.EncodeToString(mac.Sum(nil))
    return params
}
```

### Where signature is required

| Call | Client | Config |
|---|---|---|
| `POST /api/3d/creations/generations` | Main SPA | `signature: true` → signature fields go on **query string** (`params`) |
| Concept `/game3d/*` generation posts | Concept Va | **No `signature:true` observed** — cookie auth only |

Concept Va also injects `requestId` into every JSON body:

```js
Va.addRequestInterceptor((cfg) => {
  if (cfg.data?.fbxUrl?.endsWith(".glb")) {
    cfg.data.glbUrl = cfg.data.fbxUrl;
    delete cfg.data.fbxUrl;
  }
  cfg.data = { ...cfg.data, requestId: kr() };
  return cfg;
});
```

If `fbxUrl` ends with `.glb`, it is rewritten to `glbUrl` before send.

---

## 3. NEW generation tasks — Concept pipeline (`/game3d/...`)

These are the **task-creating** endpoints (not `regenerate_work`).  
Each returns `{ errNo, errMsg, data: { worksId: string, ... } }` with `errNo === 0` on success.  
Then the UI polls `get_works_list` by `worksIds:[worksId]` until `pipelineStatus` is terminal.

### 3.1 Concept design

| Method | Path | Request body fields (from callers) |
|---|---|---|
| POST | `/api/game3d/concept_design/text2image` | `prompt`, `style`, `apose` (optional bool) |
| POST | `/api/game3d/concept_design/image2views` | `imageUrl`, `style`, `apose` (optional) |

Style enum values seen: `""`, `q_version`, `steampunk`, `futuristic`, `pixel`, `hand_drawn`, `low-poly`.

Call site (`index-CdHjtkz5.js`):

```js
const a = { style: M.value, apose: D };
if (mode === "text2image") await post("/game3d/concept_design/text2image", { prompt: E, ...a });
if (mode === "image2views") await post("/game3d/concept_design/image2views", { imageUrl: R, ...a });
```

### 3.2 Geometry generation (`GeometryLeftPanel-BEIfkEln.js`)

| Method | Path | Request fields |
|---|---|---|
| POST | `/api/game3d/geometry_generation/text2geometry` | `prompt`, `modelVersion`, `apose?`, `faceCount` (number) |
| POST | `/api/game3d/geometry_generation/image2geometry` | `singleImage`, `modelVersion`, `dependOnWorksId?`, `faceCount`, `boundingBoxList?`, `apose?` |
| POST | `/api/game3d/geometry_generation/views2geometry` | `modelVersion`, `viewsImage`, `faceCount`, `apose?` |
| POST | `/api/game3d/geometry_generation/5views2geometry` | `singleImage`, `modelVersion`, `dependOnWorksId?`, `viewsImage`, `faceCount` |
| POST | `/api/game3d/geometry_generation/image2geometry_singleimage` | (English single-image path; defined, call not in this chunk) |
| POST | `/api/game3d/geometry_generation/image2geometry_views` | (English multi-view path; defined, call not in this chunk) |

`viewsImage` shape (explicit object in older path):

```js
{
  frontImageUrl, backImageUrl, leftImageUrl, rightImageUrl
  // conceptDesignRsp.viewsData may also carry more faces
}
```

`modelVersion` values: `v3.0`, `v3.1`, `v3.5`, `v3.5-preview` (UI default `v3.5-preview` when feature-flagged, else `v3.1`).

`faceCount` default UI value often `5e5` (500000). Face-count slider uses values like `[100,100,100]` in local state — callers send `Number(_)`.

Response: `{ errNo, data: { worksId } }`.

### 3.3 Component splitting (concept_index)

| Method | Path | Request extras |
|---|---|---|
| POST | `/api/game3d/component_splitting/v2/pre_segmentation` | body + `mode: "full_segmentation"` |
| POST | `/api/game3d/component_splitting/v2/pre_segmentation` | body + `mode: "interactive_segmentation"` |
| POST | `/api/game3d/component_splitting/v2/part_split` | body + `userId` (from user store) |
| POST | `/api/game3d/component_splitting/v2/smooth_segmentation` | body |
| POST | `/api/game3d/component_splitting/v1_5/part_split` | body |
| POST | `/api/game3d/component_splitting/part_split` | body |
| POST | `/api/game3d/component_splitting/part_merge` | body |
| POST | `/api/game3d/component_splitting/part_adjust` | body |
| POST | `/api/game3d/component_splitting/update_component_splitting` | body |

Typical fields carried in `e`: `fbxUrl`/`glbUrl`, `worksId` / `dependOnWorksId`, segmentation masks / part ids (UI-dependent). `requestId` is auto-injected.

### 3.4 Bone skinning

```
POST /api/game3d/bone_skinning/bone_skinning
body: { fbxUrl, isWithBone, isKeepBone, dependOnWorksId }
response.data.worksId
```

Face-count guard client-side: models with `faceCount > 500000` are blocked for bone skinning.

### 3.5 UV unwrapping

```
POST /api/game3d/uv_unwrapping/uv_unwrapping
body: { fbxUrl, dependOnWorksId }
```

### 3.6 Low-poly topology

```
POST /api/game3d/low_poly_topology/low_poly_topology
body: { fbxUrl, polygonType, detailLevel, modelVersion, dependOnWorksId?, numOfFaces? }

POST /api/game3d/low_poly_topology/image2low_poly
body: { imageUrl, numOfFaces, polygonType, modelVersion }
```

`modelVersion` low-poly enum includes `v1.5`, `v2.0`, `flash` (module `Et`).

### 3.7 Motion generation / retarget

```
POST /api/game3d/motion_generation/text2motion
body: { textPrompt, duration, disableRewrite, dependOnWorksId?, retargetFbx? }

POST /api/game3d/motion_generation/video2motion
body: { videoUrl, dependOnWorksId?, retargetFbx? }

POST /api/game3d/motion_retarget/motion_retarget
body: { motionType, worksId?, fbxUrl?, dependOnWorksId?, imageUrl? }
```

Preset-template retarget uses `{ motionType, worksId, fbxUrl }` or `{ motionType, dependOnWorksId?, fbxUrl, imageUrl }`.

### 3.8 Texture painting (`utils-Duo5gWek.js` + `index-B-iC2H9C.js`)

Version switch: if preview flag on → `*_v35preview` / `*_v35`, else `*_v31`.

| Path | Request fields seen |
|---|---|
| `/api/game3d/texture_painting/image2texture_v35preview` \| `image2texture_v31` | `fbxUrl`, `imageUrl`, `dependOnWorksId`, `keepUv` |
| `/api/game3d/texture_painting/views2texture_v35preview` \| `views2texture` | `fbxUrl`, `viewsImage`, `dependOnWorksId`, `keepUv` |
| `/api/game3d/texture_painting/text2texture` \| `text2texture_v31` | `fbxUrl`, `prompt`, `dependOnWorksId` |
| `/api/game3d/texture_painting/texture_segmentation` | body passthrough |
| `/api/game3d/texture_painting/texture_edit_local` | `fbxUrl`, `worksId`, `editType`, `prompt?`, `imageUrl?`, `viewsImage?`, `localNames` |
| `/api/game3d/texture_painting/update_texture_painting` | body passthrough |

`editType` numeric enum (from `utils-Duo5gWek.js`):

```
TEXT_2_TEXTURE = 1
MATERIAL = 2
IMAGE_2_TEXTURE = 3
PBR_PROPERTY = 4
VIEWS_2_TEXTURE = 5
TEXT_2_TEXTURE_v35 = 6
IMAGE_2_TEXTURE_v35 = 7
VIEWS_2_TEXTURE_v31 = 8
```

Operation name strings: `image2texture`, `text2texture`, `magicbrush`.

---

## 4. Works list / regenerate / quota (Concept)

### 4.1 Response envelope

Concept Va response interceptor:

```js
const { data } = response;
if (data.errNo == null) return response; // passthrough
if (data.errNo === 0) return response;
throw new Error(data.errMsg || "请求失败");
```

So business payloads are always:

```json
{ "errNo": 0, "errMsg": "", "data": { ... } }
```

HTTP 401 → redirect `/login?redirect_url=...`.

### 4.2 `get_works_list`

```
POST /api/game3d/general_info/get_works_list
body (spread of caller arg):
  {
    worksPipeline?: number,   // ut enum, optional
    worksIds?: string[],      // optional
    pipelineStatus?: number   // dt enum, optional
  }
response:
  { errNo, errMsg, data: { list: Work[] } }
```

Observed caller patterns:

```js
getWorksList({ worksPipeline, worksIds })
getWorksList({ worksPipeline: ut.CONCEPT, pipelineStatus: dt.SUCCESS })
getWorksList({ worksIds: [worksId] })
getWorksList({ worksPipeline, pipelineStatus: dt.SUCCESS })
```

### 4.3 `worksPipeline` enum (`ut`)

```
ALL = 0
CONCEPT = 1
GEOMETRY = 2
COMPONENT = 3
POLY = 4
UV = 5
TEXTURE = 6
RIGGING_AND_SKINNING = 7
ANIMATION_EFFECT = 8
```

### 4.4 `pipelineStatus` enum (`dt`)

```
PENDING = 0
PROCESSING = 1
SUCCESS = 2
FAILED = 3
CANCEL = 4
DELETED = 5
```

Polling treats `SUCCESS|FAILED|CANCEL` as terminal; `PROCESSING|PENDING` stay in poll list. UI also compares `aheadTaskNum` for queue position.

### 4.5 Work item fields (from consumers)

```
worksId
worksPipeline          // ut
pipelineStatus         // dt
aheadTaskNum
modelVersion
inputInfo              // { prompt?, videoUrl?, motionType?, ... } (may be encrypted; VM() decrypts)
sourceImageUrl
singleImage
viewsImage             // { frontImageUrl, backImageUrl, leftImageUrl, ... }
nodeList               // completed pipeline nodes (ut values)
preNodeInfoList
dependonWorksInfo      // e.g. { conceptDesignWorksId, worksId }
worksType
modelInfo              // nested per-stage responses
```

### 4.6 `modelInfo` stage responses

```
conceptDesignRsp
  .imageUrl
  .viewsData.frontImageUrl / backImageUrl / ...
geometryGenerationRsp
  .glbUrl, .fbxUrl, .imageUrl
componentSplittingRsp
  .glbUrl, .fbxUrl, .partGlbUrl, .partFbxUrl, .imageUrl
lowPolyTopologyRsp
  .glbUrl, .bakeNormal...
boneSkinningRsp
  .rigFbxUrl, .rigImageUrl, .isCharacter, .isKeepBone
motionRetargetRsp / motionGenerationRsp
  .retargetFbxUrl, .fbxUrl, ...
texturePaintingRsp
  .operateInfo  // e.g. "TextureSegmentation"
```

Defaulting when missing: UI does `conceptDesignRsp: e.conceptDesignRsp || {}` etc.

### 4.7 `regenerate_work`

```
POST /api/game3d/general_info/regenerate_work
body: passthrough object `e`  (no call site with field names found in these bundles)
```

API is registered as `regenerateWork(e) => Va.post(..., e)`. **No frontend call site** in `concept_index.js` or chunks — likely used from another entry or legacy. Treat body as opaque; proxy should forward JSON as-is.

### 4.8 `get_remaining_times`

```
POST /api/game3d/general_info/get_remaining_times
body: (none / empty)
response.data.remainingTimes   // number → stored as restTime
```

Caller:

```js
const { data: { remainingTimes } } = await ic.getRemainingTimes();
gB.setState({ restTime: remainingTimes });
```

### 4.9 Other general_info / works / share

```
POST /api/game3d/general_info/get_motion_case
POST /api/game3d/general_info/get_good_case
POST /api/game3d/general_info/delete_works
  body: { worksIds: string[] }

POST /api/game3d/share/create
  body: { worksId, modelUrl }

GET  /api/game3d/share/detail?shareToken={shareToken}
  → { errNo, errMsg, data: Work | null }
```

### 4.10 Feedback / whitelist / user

```
POST /api/game3d/feedback/like|dislike|cancel|get
POST /api/game3d/feedback/get_dislike_reasons

POST /api/game3d/resource/upload
POST /api/game3d/resource/download          // responseType blob
POST /api/game3d/resource/character_validations
POST /api/game3d/resource/format_conversions

GET  /api/3d/getuserinfo                    // concept user info (note: /3d/ not /game3d/)

POST /api/new-portal/whitelist/query
  body: { business: "3dgame", scene: "all" }
POST /api/new-portal/whitelist/apply
  body: { business: "3dgame", scene: "all", message: JSON.stringify(userMsg) }
```

---

## 5. Main SPA `/api/3d/*` surface (module 38455)

Export map → path (headers arg usually `{...t}` optional extra headers):

| Export | Method | Path | Notes |
|---|---|---|---|
| `zj` (r) | GET | `/api/3d/config` | `useCache: true` |
| `Dg` (i) | POST | `/api/3d/resource/genUploadInfo` | body `{ fileName }`; `data` = COS upload info |
| **`ar` (l)** | **POST** | **`/api/3d/creations/generations`** | **`signature: true`**; request body from caller; **creates a generation task** |
| `EA` (s) | GET | `/api/3d/creations/detail?creationsId={id}` | poll detail |
| `zS` (c) | POST | `/api/3d/resource/review` | |
| `F4` (d) | POST | `/api/3d/resource/cmykCheck` | |
| `aC` (u) | POST | `/api/3d/creations/boneBindingFromMesh` | |
| `rH` (m) | POST | `/api/3d/quotainfo` | quota |
| `tI` (p) | GET | `/api/3d/planCreditInfo` | |
| `eQ` (g) | GET | `/api/3d/purchaseList` | |
| `rv` (h) | POST | `/api/3d/purchaseBasicPlan` | no body |
| `oC` (f) | GET | `/api/open/auth?appID=&code=&signature=` | open-platform auth |
| `O3` (y) | POST | `/api/3d/creations/count` | |
| `o0` (w) | POST | `/api/3d/creations/retopologize` | |
| `Se` (v) | POST | `/api/3d/creations/delete` | |
| `wD` (T) | POST | `/api/3d/feedback/submit` | |
| `Jq` (E) | POST | `/api/3d/creations/manualBoneBindingFromMesh` | |
| `gj` (b) | POST | `/api/3d/creations/manualBoneBinding` | |
| `V6` (S) | POST | `/api/3d/creations/cancel` | |
| `Jd` (A) | GET | `/api/3d/interaction/config` | |
| `DP` (D) | GET | `/api/3d/worldPlay/config` | |
| `ne` (k) | POST | `/api/3d/notice/list` | |
| `O4` (I) | POST | `/api/3d/notice/statusUpdate` | |
| `dZ` (x) | POST | `/api/3d/worldPlay/text2Image` | |
| `xh` (R) | POST | `/api/3d/worldScene/review` | |
| (89447) | POST | `/api/3d/creations/resourceConvert` | |
| (89447) | POST | `/api/3d/worldPlay/page2video` | `responseType: blob` |

Wrapper module **56146**: in-flight dedupe by function id + JSON args (not a debounce).

### `genUploadInfo` caller shape

```js
const resp = await Dg({ fileName }, extraHeaders);
cosInfo = resp.data; // used for Tencent COS upload
```

COS hosts referenced:

- `hunyuan-base-prod-1258344703.cos.ap-guangzhou.myqcloud.com`
- `hunyuan-base-test-1258344703.cos.ap-guangzhou.myqcloud.com`
- accelerate variants `*.cos.accelerate.myqcloud.com`
- `hunyuan-3d-backend-dj-1362614848.cos.ap-tokyo.myqcloud.com` (global)

### `creations/generations` payload clues

Main bundle does **not** include the page that builds the body (lazy chunk). Observed related identifiers:

- Tracker/business type strings: `text2ModelV3.1`, `image2ModelV3.1`, `multiView2ModelV3.1`, `text2GeometryV3.1`, `image2GeometryV3.1`, `textureV3.1`, `text2ModelV3.5`, `image2ModelV3.5`, `text2GeometryV3.5`, `image2GeometryV3.5`, low-poly variants, `modelSketchV3.1`
- Historical creation parent fields: `businessType`, `imageList`, `prompt`
- Detail poll key: `creationsId`

Because the call uses `signature: true`, the signed fields are **query params** added by interceptor (timestamp/nonce/sign + any existing `cfg.params`). Body remains the generation JSON.

### `quotainfo` / plan credit UI fields

```js
// plan credit context defaults seen in main
{ quota:0, alarmQuota:0, remainQuota:0, consumeQuota:0,
  showUserInviteQuotaTag:false, perUserInviteQuotaCount:0,
  maxUserInviteQuota:0, userInviteQuota:0 }
```

i18n: `"Free usage: {{remainQuota}}/{{totalQuota}}"`.

`get_remaining_times` (concept) is the simpler counter (`remainingTimes`).

---

## 6. Login / cookies / QR

### 6.1 Cookie & storage names (authoritative)

| Name | Kind | Role |
|---|---|---|
| **`hy_user`** | Cookie | User id; read into header `x_hunyuan_inner_user_id` |
| **`hy_token`** | Cookie | Session / auth token |
| **`userInfo`** | localStorage (also cookie-access via cookie lib) | Parsed user object (`userId`, `completePhoneNumber`, `email`, `userType`, `isGlobalVersion`) |
| **`hy_global_config`** | localStorage | Global config |
| **`hy_device_id`** | localStorage | Device id (also used in cache key hashing) |
| **`__BEACON_deviceId`** | localStorage | Beacon analytics device id |
| **`user_login_emails`** | localStorage | Email login history |
| **`hideBulletinDialog_ploy_gen`** | localStorage | UI flag |

Login SDK (tencent-sdk) localStorage keys:

| Key | Role |
|---|---|
| `hyc-login-nonce` | OAuth nonce |
| `hyc-login-target-url` | Post-login redirect |
| `hyc-login-fail-target-url` | Fail redirect |
| `hyc-login-last-type` | Last login method (`phone`/`qq`/`wechat`/`email`) |
| `hyc-login-extra-payload` | Extra bind/login payload JSON |
| `hyc-login-app-id` | Cached appid JSON from `/api/jointappid` |

Axios cache: localStorage prefix **`axios-cache:`**.

i18n: `localStorage["i18n_language"]` (and query `lang`).

### 6.2 AppIds

| AppId | Use |
|---|---|
| **`4591`** | Beacon / ABTest init (`appId:"4591"`) — **not** the QQ/WeChat login appid |
| Dynamic | Login appid fetched from **`GET /api/jointappid`** (default path prop `getAppId="/api/jointappid"`); response cached in `hyc-login-app-id` |
| Open auth | Caller supplies `appID` to `GET /api/open/auth?appID=&code=&signature=` |
| COS `AppId` | Legacy COS SDK field (deprecated; bucket style `name-appid`) |

### 6.3 Login methods (SDK)

Login type enum: `phone`, `qq`, `wechat`, `email`.

**QQ OAuth** (redirect, not XHR poll in this SDK):

```
https://graph.qq.com/oauth2.0/authorize
  ?response_type=code
  &client_id={appid}
  &redirect_uri={redirectUrl}
  &state={qq_login|qq_bind}   // also appends ?nonce={nonce}&isMobile=0|1
  &display=
```

Nonce stored in `hyc-login-nonce` before jump.

**WeChat**: loads `//res.wx.qq.com/connect/zh_CN/htmledition/js/wxLogin.js`, then `new WxLogin({ self_redirect:false, id, appid, scope:"snsapi_login", redirect_uri, ... })` into a div. QR is rendered by Tencent's widget; **poll endpoint is inside wxLogin.js / WeChat OAuth**, not in this bundle.

**Phone**: TencentCaptcha (`window.TencentCaptcha` + script), then confirm payload:

```js
{ phoneNumber, verificationCode, appId }
```

**Email**:

```js
{ email, verificationCode }
```

**Open auth** (main SPA):

```
GET /api/open/auth?appID={appID}&code={code}&signature={signature}
```

`signature` here is the open-platform signature passed by the caller (distinct from module 47436 HMAC unless the caller reuses it).

### 6.4 QR poll endpoints

**Not present** as first-party paths in these bundles:

- No `qrlogin`, `qr_status`, `ptqrshow`, `ptqrlogin` strings in main/concept.
- Concept only has i18n `qrcode.expiredText` ("二维码过期").
- Main SPA has route `SCAN = "/scan"` (lazy chunk 5241) — content not in downloaded files.
- WeChat/QQ QR auth is delegated to official OAuth pages; the site receives `code` on redirect then calls its own session establish APIs (cookie `hy_user`/`hy_token` set by backend).

For a Go proxy: rely on **cookie replay** (`hy_user` + `hy_token`) rather than reimplementing QR poll. `/api/3d/getuserinfo` (concept) or main user bootstrap is the session check.

### 6.5 Config decrypt (related)

Module 59449 AES helper used for encrypted remote config / user config blobs:

```js
key = CryptoJS.enc.Utf8.parse(passphrase || "4IeT9G6Pfg2hpn3F") // 16 bytes
payload = CryptoJS.enc.Base64.parse(ciphertext)
iv  = first 4 words (16 bytes) of payload
body = remaining words
plain = AES.decrypt({ciphertext: body}, key, { iv, mode: CBC, padding: Pkcs7 }).toString(Utf8)
```

`inputInfo` on works may be encrypted and is decrypted client-side via `VM()`.

---

## 7. Hosts

| Host | Role |
|---|---|
| `hunyuan.tencent.com` | Prod web (CN) |
| `hunyuanglobal.com` | Prod web (global) |
| `3d.global.{test,pre}.hunyuan.woa.com` | Internal global |
| `hunyuan-3d-{test,pre,}.woa.com` | Internal CN |
| `3d.{test,pre}.hunyuan.woa.com` | Internal |
| `cdn-game-3d.qstatic.com/game3d/assets/` | SPA/COS public assets |
| `*.cos.ap-guangzhou.myqcloud.com` | Model/COS storage |
| `beacon.qq.com`, `aegis.qq.com`, … | Analytics (not API) |

Webpack chunk name: `hunyuan_3d_web`.

---

## 8. Go reverse-proxy checklist

1. **Upstream base**: same origin as the SPA (no separate API host in JS). Paths:
   - Main: `/api/3d/*`, `/api/open/auth`
   - Concept: `/api/game3d/*`, `/api/3d/getuserinfo`, `/api/new-portal/*`
2. **Cookies**: forward `hy_user`, `hy_token` (and any `hy_*` / session cookies the backend sets).
3. **Headers**: inject/forward `x-source: web`, `x-product: hunyuan3d`, `Content-Type: application/json`, optional `x_hunyuan_inner_user_id` (= `hy_user`), `Trace-Id`.
4. **Signature** (only for main `creations/generations` and any other call you mark `signature:true`):
   - Add `timestamp`, `nonce`, `sign` as **query parameters**.
   - HMAC-SHA256, key `Hf6d6KFB3D`, Base64, canonical = sorted non-empty `k=v` joined by `&`.
5. **Concept generation create** = stage-specific `POST /api/game3d/<stage>/<op>` returning `data.worksId`; **not** a single generic create. Poll via `get_works_list`.
6. **Response unwrap**: concept uses `{errNo, errMsg, data}`; treat `errNo!=0` as business error.
7. **Body rewrite**: if proxying concept Va traffic, `requestId` is expected in bodies; `fbxUrl` ending in `.glb` should be sent as `glbUrl`.
8. **401** → client redirects to `/login?redirect_url=...`; proxy should not swallow 401.
9. **CORS/CORS-less**: same-origin; if proxy is cross-origin, must allow credentials and cookie names.
10. **No QR poll API** in-bundle — session is cookie-based after OAuth redirect.

---

## 9. Raw artifacts written by this dive

| Path | Content |
|---|---|
| `D:/lose/docs/module-91786.txt` | Full axios module source |
| `D:/lose/docs/module-47436.txt` | Full signature module source |
| `D:/lose/docs/module-38455.txt` (via `_api-module-main.txt`) | Main `/api/3d/*` API module |
| `D:/lose/docs/_api-module-main.txt` | Same |
| `D:/lose/docs/_main-api-surface.txt` | Flat list of main API paths |
| `D:/lose/docs/_chunk-endpoints.txt` | Concept chunk endpoint contexts |
| `D:/lose/docs/_search-report.txt` | Pattern search dump |
| `D:/lose/docs/_auth-context.txt` | Auth/cookie string contexts |
| `D:/lose/docs/_extract4.txt` / `_extract5` logs | Call-site dumps |
| `D:/lose/output/playwright/hunyuan/concept_chunks/*` | Downloaded SPA chunks |

---

## 10. Cross-check vs existing proxy docs

Already present in `D:\lose\docs\`:

- `signature.md` — same HMAC secret `Hf6d6KFB3D`, same XOR/ROTL/perm key derivation. Matches this dive.
- `proxy-api.md` — local `hunyuan3d-proxy` on `:8787`, upstream `https://3d.hunyuan.tencent.com`, modes `main|concept`.
- Implementation: `D:\lose\hunyuan3d-proxy\signature.go`.

**Discrepancy to flag:** `signature.md` claims concept `/game3d/*` generation posts are signed. In the JS bundles, **only** main `POST /api/3d/creations/generations` sets `signature:true`. Concept `Va` has **no signature interceptor** — concept generation posts are cookie-auth only. If the proxy force-signs all concept POSTs it is doing more than the official SPA; that is usually harmless (extra query params) but body/query field sets may differ from SPA traffic.

---

## 11. Residual gaps

- Exact JSON body schema for **main** `POST /api/3d/creations/generations` lives in a lazy SPA chunk not present in `index__aa01d2040ee35d2c8c78.js`; only business-type labels and `signature:true` are confirmed.
- `regenerate_work` has no in-bundle call site (field names unknown).
- `/scan` route implementation (possible QR) not downloaded.
- Concept `Va` axios create call shown as `gr.create({ baseURL: e, timeout: A })` with construction `(... )("/api")` — `withCredentials` not explicitly set; cookies still flow same-origin.
- WeChat/QQ QR **poll** endpoints are inside third-party SDK scripts, not these bundles.
