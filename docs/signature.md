# 混元3D Web 签名算法（模块 47436 / u.Xv）

主站 `POST /api/3d/creations/generations` 在 axios 拦截器中对 **query params** 附加签名。

## 结论

| 项 | 值 |
|----|----|
| 算法 | `Hex(HmacSHA256(canonical, key))` |
| 密钥 | `Hf6d6KFB3D` |
| 字段 | `timestamp`（unix 秒）、`nonce`（16 位 `[A-Za-z0-9]`）、`sign`（hex 小写） |
| 使用范围 | 仅主站签名接口；concept `/game3d/*` **不加签** |

## 步骤

```
1. g = copy(params)
2. g.timestamp = floor(unix_now)
3. g.nonce = random 16 chars [A-Za-z0-9]
4. 过滤 null / ""，值转字符串
5. 按 key 做 localeCompare 升序
6. canonical = "k=v&k=v"
7. sign = Hex(HmacSHA256(canonical, "Hf6d6KFB3D"))
8. 将 sign/timestamp/nonce 合并回 query
```

## 密钥派生

```
c = [122,59,92,165,30,79,166,139,142,129,139,89,219,131,101,204]
d = [122,59,92,45,30,79,106,139,156,13,46,63,74,91,108,125]
u = [3,5,2,7,1,4,6,2,5,3,1,4,2,6,3,5]
m = [14,11,13,9,15,10,12,8,6,3,5,1,7,2,4,0]

t[i] = c[i] XOR d[i]
o[i] = rotl8(t[i], u[i])
n[i] = o[m[i]]
key  = UTF-8(n 截断到第一个 0x00)
→ "Hf6d6KFB3D"
```

## Go 实现

```go
import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/hex"
)

key := []byte("Hf6d6KFB3D")
mac := hmac.New(sha256.New, key)
mac.Write([]byte(canonical))
sign := hex.EncodeToString(mac.Sum(nil))
```

## 请求头（主站）

```
Content-Type: application/json
x-source: web
x-product: hunyuan3d
```

可选：`x_hunyuan_inner_user_id` = `hy_user`

## 调试

```text
GET /api/sign/debug
GET /api/health   # 含 signKey / signKeyHex
```
