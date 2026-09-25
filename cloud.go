package main

// Official Tencent Cloud Hunyuan 3D (ai3d) API 3.0 adapter.
// Docs: https://cloud.tencent.com/document/product/1804/120829
// Overview: https://cloud.tencent.com/document/product/1804/120838
// Common params: https://cloud.tencent.com/document/api/1804/120832
// Signature v3 (TC3-HMAC-SHA256): https://cloud.tencent.com/document/api/1804/120833
//
// This path is INDEPENDENT of the web reverse-proxy (main/concept modes).
// Credentials: TENCENT_SECRET_ID / TENCENT_SECRET_KEY (or per-request headers).

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	cloudHost    = "ai3d.tencentcloudapi.com"
	cloudVersion = "2025-05-13"
	cloudService = "ai3d"
	cloudDefaultRegion = "ap-guangzhou"
)

// cloudActions maps local route suffix → official Action name.
// Source: https://cloud.tencent.com/document/product/1804/120838
var cloudActions = map[string]string{
	"submit-pro":         "SubmitHunyuanTo3DProJob",
	"query-pro":          "QueryHunyuanTo3DProJob",
	"submit-rapid":       "SubmitHunyuanTo3DRapidJob",
	"query-rapid":        "QueryHunyuanTo3DRapidJob",
	"submit-texture":     "SubmitTextureTo3DJob",
	"query-texture":      "DescribeTextureTo3DJob",
	"submit-reduce-face": "SubmitReduceFaceJob",
	"query-reduce-face":  "DescribeReduceFaceJob",
	"submit-part":        "SubmitHunyuan3DPartJob",
	"query-part":         "QueryHunyuan3DPartJob",
	"submit-uv":          "SubmitHunyuanTo3DUVJob",
	"query-uv":           "DescribeHunyuanTo3DUVJob",
	"submit-motion":      "SubmitHunyuanTo3DMotionJob",
	"query-motion":       "DescribeHunyuanTo3DMotionJob",
	"submit-rigging":     "SubmitAutoRiggingJob",
	"query-rigging":      "DescribeAutoRiggingJob",
	"submit-profile":     "SubmitProfileTo3DJob",
	"query-profile":      "DescribeProfileTo3DJob",
	"convert-format":     "Convert3DFormat",
}

func cloudSecretID(r *http.Request) string {
	if v := r.Header.Get("X-Secret-Id"); v != "" {
		return v
	}
	return os.Getenv("TENCENT_SECRET_ID")
}

func cloudSecretKey(r *http.Request) string {
	if v := r.Header.Get("X-Secret-Key"); v != "" {
		return v
	}
	return os.Getenv("TENCENT_SECRET_KEY")
}

func cloudRegion(r *http.Request) string {
	if v := r.URL.Query().Get("region"); v != "" {
		return v
	}
	if v := r.Header.Get("X-TC-Region"); v != "" {
		return v
	}
	if v := os.Getenv("TENCENT_REGION"); v != "" {
		return v
	}
	return cloudDefaultRegion
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

func hashSHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// tc3Signature builds TC3-HMAC-SHA256 Authorization header value.
// Algorithm: https://cloud.tencent.com/document/api/1804/120833
func tc3Signature(secretID, secretKey, action, region string, ts int64, payload []byte) (auth, date string) {
	date = time.Unix(ts, 0).UTC().Format("2006-01-02")
	// 1. CanonicalRequest
	host := cloudHost
	ct := "application/json; charset=utf-8"
	canonicalHeaders := fmt.Sprintf("content-type:%s\nhost:%s\nx-tc-action:%s\n", ct, host, strings.ToLower(action))
	signedHeaders := "content-type;host;x-tc-action"
	hashedPayload := hashSHA256Hex(payload)
	canonicalRequest := strings.Join([]string{
		"POST",
		"/",
		"",
		canonicalHeaders,
		signedHeaders,
		hashedPayload,
	}, "\n")
	// 2. StringToSign
	credentialScope := date + "/" + cloudService + "/tc3_request"
	stringToSign := strings.Join([]string{
		"TC3-HMAC-SHA256",
		strconv.FormatInt(ts, 10),
		credentialScope,
		hashSHA256Hex([]byte(canonicalRequest)),
	}, "\n")
	// 3. Signature
	secretDate := hmacSHA256([]byte("TC3"+secretKey), date)
	secretService := hmacSHA256(secretDate, cloudService)
	secretSigning := hmacSHA256(secretService, "tc3_request")
	signature := hex.EncodeToString(hmacSHA256(secretSigning, stringToSign))
	// 4. Authorization
	auth = fmt.Sprintf(
		"TC3-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		secretID, credentialScope, signedHeaders, signature,
	)
	return auth, date
}

// HandleCloud proxies an official ai3d Action to ai3d.tencentcloudapi.com.
// Local path: /api/cloud/{action-slug}  or  /api/cloud/action/{ActionName}
func (s *Server) HandleCloud(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/cloud")
	rest = strings.TrimPrefix(rest, "/")
	rest = strings.Trim(rest, "/")

	var action string
	switch {
	case rest == "":
		writeOK(w, map[string]interface{}{
			"product":  "ai3d",
			"version":  cloudVersion,
			"host":     cloudHost,
			"docs":     "https://cloud.tencent.com/document/product/1804/120838",
			"actions":  cloudActions,
			"credentialed": cloudSecretID(r) != "",
		})
		return
	case strings.HasPrefix(rest, "action/"):
		action = strings.TrimPrefix(rest, "action/")
	case rest == "actions":
		writeOK(w, map[string]interface{}{"actions": cloudActions, "docs": "https://cloud.tencent.com/document/product/1804/120838"})
		return
	default:
		a, ok := cloudActions[rest]
		if !ok {
			// allow raw Action name as last path segment
			if _, known := cloudActions[rest]; !known && !strings.Contains(rest, "Hunyuan") && !strings.Contains(rest, "3D") {
				writeErr(w, http.StatusNotFound, "unknown cloud action slug "+rest+"; GET /api/cloud for list")
				return
			}
			action = rest
		} else {
			action = a
		}
	}

	sid := cloudSecretID(r)
	skey := cloudSecretKey(r)
	if sid == "" || skey == "" {
		writeErr(w, http.StatusUnauthorized,
			"cloud credentials required: set TENCENT_SECRET_ID/TENCENT_SECRET_KEY or headers x-secret-id / x-secret-key; docs https://cloud.tencent.com/document/product/1804/120829")
		return
	}

	region := cloudRegion(r)
	body, _ := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if len(body) == 0 {
		body = []byte("{}")
	}
	// Ensure body is valid JSON object
	var obj map[string]interface{}
	if err := json.Unmarshal(body, &obj); err != nil {
		writeErr(w, http.StatusBadRequest, "request body must be JSON object: "+err.Error())
		return
	}
	payload, _ := json.Marshal(obj)

	ts := time.Now().Unix()
	auth, _ := tc3Signature(sid, skey, action, region, ts, payload)

	req, err := http.NewRequest(http.MethodPost, "https://"+cloudHost+"/", bytes.NewReader(payload))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Host", cloudHost)
	req.Header.Set("X-TC-Action", action)
	req.Header.Set("X-TC-Version", cloudVersion)
	req.Header.Set("X-TC-Timestamp", strconv.FormatInt(ts, 10))
	req.Header.Set("X-TC-Region", region)
	req.Header.Set("Authorization", auth)
	if token := r.Header.Get("X-TC-Token"); token != "" {
		req.Header.Set("X-TC-Token", token)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "cloud api error: "+err.Error())
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))

	var parsed interface{}
	if json.Unmarshal(respBody, &parsed) == nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-Hy-Cloud-Action", action)
		w.WriteHeader(resp.StatusCode)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":     resp.StatusCode < 400,
			"source": "tencent-cloud-ai3d",
			"docs":   "https://cloud.tencent.com/document/product/1804/120838",
			"action": action,
			"region": region,
			"version": cloudVersion,
			"data":   parsed,
		})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(respBody)
}
