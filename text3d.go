package main

// Text-to-3D helper endpoints wrapping verified upstream request shapes.
// Concept APIs accept richer bodies (requestId/worksType/modelVersion...).
// Main /api/3d/creations/generations still requires upstream configInfo.

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

type text3dRequest struct {
	Prompt       string `json:"prompt"`
	Style        string `json:"style,omitempty"`        // concept text2image: q_version|steampunk|hand_drawn|...
	ModelVersion string `json:"modelVersion,omitempty"` // v3.0 | v3.1 | v3.5-preview
	FaceCount    int    `json:"faceCount,omitempty"`
	APose        bool   `json:"apose,omitempty"`
	WorksType    *int   `json:"worksType,omitempty"`
	Mode         string `json:"mode,omitempty"` // concept-design | text-geometry | both
}

// HandleText3DGenerate POST /api/text3d/generate
// Submits concept text2image and/or text2geometry via the Go proxy session.
func (s *Server) HandleText3DGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	var req text3dRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		writeErr(w, http.StatusBadRequest, "prompt is required")
		return
	}
	if req.ModelVersion == "" {
		req.ModelVersion = "v3.1"
	}
	if req.FaceCount <= 0 {
		req.FaceCount = 500000
	}
	if req.Style == "" {
		req.Style = "hand_drawn"
	}
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		mode = "both"
	}
	wt := 0
	if req.WorksType != nil {
		wt = *req.WorksType
	}
	requestID := "go" + randomNonce(12)

	results := map[string]interface{}{
		"prompt":       req.Prompt,
		"mode":         mode,
		"requestId":    requestID,
		"modelVersion": req.ModelVersion,
		"faceCount":    req.FaceCount,
		"style":        req.Style,
		"submittedAt":  time.Now().Format(time.RFC3339),
	}

	// concept design: text2image
	if mode == "both" || mode == "concept-design" || mode == "text2image" || mode == "design" {
		payload := map[string]interface{}{
			"prompt":       req.Prompt,
			"style":        req.Style,
			"apose":        req.APose,
			"worksType":    wt,
			"modelVersion": "",
			"model":        "",
			"requestId":    requestID,
		}
		raw, code, err := s.callUpstreamJSON(r, http.MethodPost, ModeConcept,
			"/api/game3d/concept_design/text2image", payload)
		results["text2image"] = map[string]interface{}{
			"upstream": "/api/game3d/concept_design/text2image",
			"status":   code,
			"body":     raw,
			"error":    errString(err),
			"payload":  payload,
		}
	}

	// geometry: text2geometry
	if mode == "both" || mode == "text-geometry" || mode == "text2geometry" || mode == "geometry" {
		payload := map[string]interface{}{
			"prompt":       req.Prompt,
			"modelVersion": req.ModelVersion,
			"faceCount":    req.FaceCount,
			"apose":        req.APose,
			"worksType":    wt,
			"model":        "",
			"requestId":    requestID,
			"generateMode": "text_to_geometry",
		}
		raw, code, err := s.callUpstreamJSON(r, http.MethodPost, ModeConcept,
			"/api/game3d/geometry_generation/text2geometry", payload)
		results["text2geometry"] = map[string]interface{}{
			"upstream": "/api/game3d/geometry_generation/text2geometry",
			"status":   code,
			"body":     raw,
			"error":    errString(err),
			"payload":  payload,
		}
	}

	// optional main generations — real SPA payload (Playwright-captured).
	// Server resolves pipeline config from modelType; empty body → configInfo not found.
	if mode == "main" || mode == "text2model" || mode == "main-text3d" {
		payload := map[string]interface{}{
			"sceneType":     "playGround3D-2.0",
			"count":         1,
			"modelType":     "text2ModelV3.1",
			"title":         req.Prompt,
			"style":         styleToMainStyle(req.Style),
			"prompt":        req.Prompt,
			"enable_pbr":    false,
			"enableLowPoly": false,
			"faceCount":     req.FaceCount,
		}
		if req.FaceCount <= 0 {
			payload["faceCount"] = 500000
		}
		if strings.EqualFold(req.ModelVersion, "v3.5") || strings.EqualFold(req.ModelVersion, "3.5") {
			payload["modelType"] = "text2ModelV3.5"
		}
		raw, code, err := s.callUpstreamJSON(r, http.MethodPost, ModeMain,
			"/api/3d/creations/generations", payload)
		results["main_generations"] = map[string]interface{}{
			"upstream": "/api/3d/creations/generations",
			"status":   code,
			"body":     raw,
			"error":    errString(err),
			"payload":  payload,
			"note":     "signed on query via pathNeedsSignature; modelType selects upstream config",
		}
	}

	// credit snapshot after submit
	results["credit_concept"] = s.ConceptCredits(r)
	results["credit_main"] = s.MainCredits(r)

	writeOK(w, results)
}

// callUpstreamJSON posts JSON to upstream using session cookies + SPA headers.
func (s *Server) callUpstreamJSON(r *http.Request, method, mode, path string, payload interface{}) (interface{}, int, error) {
	body, _ := json.Marshal(payload)
	res := s.callUpstream(r, method, mode, path, body)
	return res.Body, res.Status, nil
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// styleToMainStyle maps concept style ids to main-site texture style enum.
func styleToMainStyle(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "default", "none":
		return ""
	case "cartoon", "q_version":
		return "cartoon"
	case "stone", "sculpture":
		return "stone"
	case "celadon", "qinghua", "blue_white":
		return "celadon"
	case "china", "chinese":
		return "chinese"
	case "cyberpunk", "sci-fi":
		return "cyberpunk"
	case "hand_drawn", "handdrawn":
		return ""
	default:
		return s
	}
}

// HandleMainText3D POST /api/text3d/main — direct main-site text-to-3D.
func (s *Server) HandleMainText3D(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	var req text3dRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		writeErr(w, http.StatusBadRequest, "prompt is required")
		return
	}
	modelType := "text2ModelV3.1"
	if strings.EqualFold(req.ModelVersion, "v3.5") || strings.EqualFold(req.ModelVersion, "3.5") {
		modelType = "text2ModelV3.5"
	}
	face := req.FaceCount
	if face <= 0 {
		face = 500000
	}
	count := 4 // SPA default; upstream rejects count=1 ("count param error")
	payload := map[string]interface{}{
		"sceneType":     "playGround3D-2.0",
		"count":         count,
		"modelType":     modelType,
		"title":         req.Prompt,
		"style":         styleToMainStyle(req.Style),
		"prompt":        req.Prompt,
		"enable_pbr":    true,
		"enableLowPoly": false,
		"faceCount":     face,
	}
	raw, code, err := s.callUpstreamJSON(r, http.MethodPost, ModeMain,
		"/api/3d/creations/generations", payload)
	writeOK(w, map[string]interface{}{
		"upstream": "/api/3d/creations/generations",
		"payload":  payload,
		"status":   code,
		"body":     raw,
		"error":    errString(err),
		"credit":   s.MainCredits(r),
	})
}

// HandleMainCreationsList POST /api/text3d/main/list
func (s *Server) HandleMainCreationsList(w http.ResponseWriter, r *http.Request) {
	payload := map[string]interface{}{"limit": 10, "offset": 0, "sceneTypeList": []interface{}{}}
	if r.Body != nil {
		var in map[string]interface{}
		b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if len(b) > 0 && json.Unmarshal(b, &in) == nil {
			for k, v := range in {
				payload[k] = v
			}
		}
	}
	raw, _ := json.Marshal(payload)
	res := s.callUpstream(r, http.MethodPost, ModeMain, "/api/3d/creations/list", raw)
	writeOK(w, map[string]interface{}{
		"upstream": "/api/3d/creations/list",
		"status":   res.Status,
		"body":     res.Body,
		"raw":      res.Raw,
	})
}

// HandleText3DWorks GET /api/text3d/works?pipeline=2
// Lists concept works; handles upstream multi-line JSON envelopes.
func (s *Server) HandleText3DWorks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("pipeline")
	payload := map[string]interface{}{}
	if q != "" {
		var n int
		if json.Unmarshal([]byte(q), &n) == nil {
			payload["worksPipeline"] = n
		} else {
			payload["worksPipeline"] = q
		}
	}
	// Dedicated untruncated upstream call (list payloads are large).
	body, _ := json.Marshal(payload)
	full := s.callUpstreamRaw(r, http.MethodPost, ModeConcept, "/api/game3d/general_info/get_works_list", body)
	list := extractWorksList(full)
	summary := make([]map[string]interface{}, 0, len(list))
	for _, witem := range list {
		mi, _ := witem["modelInfo"].(map[string]interface{})
		item := map[string]interface{}{
			"worksId":        witem["worksId"],
			"worksPipeline":  witem["worksPipeline"],
			"pipelineStatus": witem["pipelineStatus"],
			"status":         witem["status"],
			"reqPath":        witem["reqPath"],
			"prompt":         witem["prompt"],
			"model":          witem["model"],
			"modelVersion":   witem["modelVersion"],
			"createdAt":      witem["createdAt"],
			"updatedAt":      witem["updatedAt"],
			"errorMsg":       "",
			"resultUrls":     map[string]string{},
		}
		urls := map[string]string{}
		if single, ok := witem["singleImage"].(string); ok && single != "" {
			urls["singleImage"] = single
		}
		if mi != nil {
			if em, ok := mi["errorMsg"].(string); ok {
				item["errorMsg"] = em
			}
			if ec, ok := mi["errorCode"]; ok {
				item["errorCode"] = ec
			}
			// concept design images
			if cd, ok := mi["conceptDesignRsp"].(map[string]interface{}); ok {
				if u, ok := cd["imageUrl"].(string); ok && u != "" {
					urls["conceptImage"] = u
				}
				if views, ok := cd["viewsData"].(map[string]interface{}); ok {
					for _, vk := range []string{"frontImageUrl", "backImageUrl", "leftImageUrl", "rightImageUrl"} {
						if u, ok := views[vk].(string); ok && u != "" {
							urls["view_"+vk] = u
						}
					}
				}
			}
			// geometry
			for _, gk := range []string{"geometryGenerationRsp", "GeometryGenerationRsp", "geometryRsp"} {
				if g, ok := mi[gk].(map[string]interface{}); ok {
					for uk, uv := range g {
						if u, ok := uv.(string); ok && (strings.Contains(uk, "Url") || strings.Contains(uk, "url")) && u != "" {
							urls[gk+"."+uk] = u
						}
					}
				}
			}
			// dump unknown result keys
			for mk, mv := range mi {
				if mm, ok := mv.(map[string]interface{}); ok {
					for uk, uv := range mm {
						if u, ok := uv.(string); ok && strings.HasPrefix(u, "http") {
							urls[mk+"."+uk] = u
						}
					}
				}
			}
		}
		item["resultUrls"] = urls
		summary = append(summary, item)
	}
	writeOK(w, map[string]interface{}{
		"upstreamPath": "/api/game3d/general_info/get_works_list",
		"filter":       q,
		"count":        len(summary),
		"works":        summary,
		"rawBytes":     len(full),
	})
}

// callUpstreamRaw is like callUpstream but returns untruncated response body.
func (s *Server) callUpstreamRaw(r *http.Request, method, mode, path string, body []byte) string {
	res := s.callUpstream(r, method, mode, path, body)
	if res.Full != "" {
		return res.Full
	}
	if res.Raw != "" && !strings.HasSuffix(res.Raw, "...(truncated)") {
		return res.Raw
	}
	// fall back: re-call without truncate by using body field if string
	if raw, ok := res.Body.(string); ok {
		return raw
	}
	return res.Raw
}

func extractWorksList(raw string) []map[string]interface{} {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	tryParse := func(s string) []map[string]interface{} {
		var env struct {
			ErrNo int `json:"errNo"`
			Data  struct {
				List []map[string]interface{} `json:"list"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(s), &env); err == nil && len(env.Data.List) > 0 {
			return env.Data.List
		}
		var env2 struct {
			Data interface{} `json:"data"`
		}
		if err := json.Unmarshal([]byte(s), &env2); err == nil {
			if m, ok := env2.Data.(map[string]interface{}); ok {
				if arr, ok := m["list"].([]interface{}); ok {
					out := make([]map[string]interface{}, 0, len(arr))
					for _, it := range arr {
						if mm, ok := it.(map[string]interface{}); ok {
							out = append(out, mm)
						}
					}
					return out
				}
			}
		}
		return nil
	}

	if list := tryParse(raw); len(list) > 0 {
		return list
	}

	// Upstream may concatenate multiple JSON docs (often an error line then success).
	// Prefer the document whose errNo==0.
	var best []map[string]interface{}
	idx := 0
	for {
		start := strings.Index(raw[idx:], `{"errNo"`)
		if start < 0 {
			start = strings.Index(raw[idx:], `{"data"`)
			if start < 0 {
				break
			}
		}
		start += idx
		// scan balanced braces
		depth := 0
		end := -1
		for i := start; i < len(raw); i++ {
			if raw[i] == '{' {
				depth++
			} else if raw[i] == '}' {
				depth--
				if depth == 0 {
					end = i + 1
					break
				}
			}
		}
		if end <= start {
			break
		}
		doc := raw[start:end]
		list := tryParse(doc)
		if len(list) > 0 && strings.Contains(doc, `"errNo":0`) {
			return list
		}
		if len(list) > len(best) {
			best = list
		}
		idx = end
	}
	if len(best) > 0 {
		return best
	}

	// last resort: line split
	for _, p := range strings.Split(raw, "\n") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if list := tryParse(p); len(list) > len(best) {
			best = list
		}
	}
	return best
}
