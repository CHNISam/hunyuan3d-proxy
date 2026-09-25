package main

// Unified generation asset board for Web UI.
// Pulls main-site creations/list + concept get_works_list, flattens
// intermediate outputs (geometry, PBR maps, concept views, glb/fbx).

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

type AssetItem struct {
	Source       string            `json:"source"` // main | concept
	ID           string            `json:"id"`
	Title        string            `json:"title,omitempty"`
	Prompt       string            `json:"prompt,omitempty"`
	Status       string            `json:"status"`
	Progress     interface{}       `json:"progress,omitempty"`
	Pipeline     interface{}       `json:"pipeline,omitempty"`
	ModelType    string            `json:"modelType,omitempty"`
	Style        string            `json:"style,omitempty"`
	CreatedAt    interface{}       `json:"createdAt,omitempty"`
	UpdatedAt    interface{}       `json:"updatedAt,omitempty"`
	Error        string            `json:"error,omitempty"`
	PreviewURL   string            `json:"previewUrl,omitempty"`
	PrimaryModel string            `json:"primaryModelUrl,omitempty"`
	Assets       map[string]string `json:"assets"` // flattened url map
	Notes        []string          `json:"notes,omitempty"`
}

// HandleAssetBoard GET /api/text3d/assets
// Query: source=main|concept|all, limit=20, offset=0, status=all|processing|success
// Includes totalCount for history paging.
func (s *Server) HandleAssetBoard(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	src := strings.ToLower(q.Get("source"))
	if src == "" {
		src = "all"
	}
	limit := 24
	if v := q.Get("limit"); v != "" {
		var n int
		if json.Unmarshal([]byte(v), &n) == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	offset := 0
	if v := q.Get("offset"); v != "" {
		var n int
		if json.Unmarshal([]byte(v), &n) == nil && n >= 0 {
			offset = n
		}
	}
	statusFilter := strings.ToLower(q.Get("status"))
	if statusFilter == "" {
		statusFilter = "all"
	}

	items := []AssetItem{}
	total := 0

	if src == "all" || src == "main" {
		mainItems, mainTotal := s.collectMainAssets(limit, offset)
		total += mainTotal
		items = append(items, mainItems...)
	}
	if src == "all" || src == "concept" {
		cItems, cTotal := s.collectConceptAssets(limit)
		total += cTotal
		items = append(items, cItems...)
	}

	if statusFilter != "all" && statusFilter != "" {
		filtered := items[:0]
		for _, it := range items {
			if strings.EqualFold(it.Status, statusFilter) {
				filtered = append(filtered, it)
			}
		}
		items = filtered
	}

	// sort newest first when timestamps present
	sortAssetsByTime(items)

	writeOK(w, map[string]interface{}{
		"source":       src,
		"statusFilter": statusFilter,
		"limit":        limit,
		"offset":       offset,
		"totalCount":   total,
		"count":        len(items),
		"items":        items,
		"credits": map[string]interface{}{
			"main":    s.MainCredits(r),
			"concept": s.ConceptCredits(r),
		},
		"endpoints": map[string]string{
			"mainList":    "POST /api/3d/creations/list",
			"conceptList": "POST /game3d/general_info/get_works_list",
			"board":       "GET /api/text3d/assets?source=&limit=&offset=&status=",
		},
		"hint": "历史会随 offset 翻页；UI 本地也会合并保留已见过的任务",
	})
}

func sortAssetsByTime(items []AssetItem) {
	getTs := func(it AssetItem) float64 {
		switch t := it.CreatedAt.(type) {
		case float64:
			return t
		case int64:
			return float64(t)
		case int:
			return float64(t)
		case string:
			if f, err := parseFloat(t); err == nil {
				return f
			}
		}
		// try raw json number string via asString
		if s := asString(it.CreatedAt); s != "" {
			if f, err := parseFloat(s); err == nil {
				return f
			}
		}
		return 0
	}
	for i := 1; i < len(items); i++ {
		j := i
		for j > 0 && getTs(items[j]) > getTs(items[j-1]) {
			items[j], items[j-1] = items[j-1], items[j]
			j--
		}
	}
}

func parseFloat(s string) (float64, error) {
	var f float64
	err := json.Unmarshal([]byte(s), &f)
	return f, err
}

// collectMainAssets returns page of main creations + total count.
func (s *Server) collectMainAssets(limit, offset int) ([]AssetItem, int) {
	payload := map[string]interface{}{
		"limit":         limit,
		"offset":        offset,
		"sceneTypeList": []interface{}{},
	}
	raw, _ := json.Marshal(payload)
	res := s.callUpstream(r2(), http.MethodPost, ModeMain, "/api/3d/creations/list", raw)

	// Prefer untruncated body: creations payloads exceed the 2KB Raw preview.
	fullBody := res.Full
	if fullBody == "" {
		fullBody = res.Raw
	}
	creations, total := extractCreationsWithTotal(fullBody, res.Body)
	if total == 0 && len(creations) == 0 {
		prefix := res.Full
		if len(prefix) > 240 {
			prefix = prefix[:240]
		}
		log.Printf("board/main empty: status=%d err=%q fullLen=%d bodyType=%T prefix=%q",
			res.Status, res.Error, len(res.Full), res.Body, prefix)
	}
	out := make([]AssetItem, 0, len(creations))
	for _, c := range creations {
		item := AssetItem{
			Source:    "main",
			Assets:    map[string]string{},
			ID:        asString(c["id"]),
			Title:     asString(c["title"]),
			Prompt:    asString(c["prompt"]),
			Status:    asString(c["status"]),
			ModelType: asString(c["modelType"]),
			Style:     asString(c["style"]),
			CreatedAt: c["createdAt"],
			UpdatedAt: c["updatedAt"],
			Pipeline:  c["sceneType"],
		}
		if arr, ok := c["result"].([]interface{}); ok && len(arr) > 0 {
			var chosen map[string]interface{}
			var allProgress []string
			for _, it := range arr {
				if m, ok := it.(map[string]interface{}); ok {
					if asString(m["status"]) == "success" && chosen == nil {
						chosen = m
					}
					if chosen == nil {
						chosen = m
					}
					st := asString(m["status"])
					pg := asString(m["progress"])
					allProgress = append(allProgress, st+":"+pg+"%")
				}
			}
			if chosen != nil {
				item.Status = asString(chosen["status"])
				if item.Status == "" {
					item.Status = asString(c["status"])
				}
				item.Progress = chosen["progress"]
				if len(allProgress) > 0 {
					item.Notes = append(item.Notes, strings.Join(allProgress, " | "))
				}
				if u, ok := chosen["urlResult"].(map[string]interface{}); ok {
					for k, v := range u {
						if str, ok := v.(string); ok && strings.HasPrefix(str, "http") {
							item.Assets[k] = str
						}
					}
				}
				if io, ok := chosen["intermediate_outputs"].(map[string]interface{}); ok {
					if g, ok := io["geometry"].(map[string]interface{}); ok {
						for k, v := range g {
							if str, ok := v.(string); ok && strings.HasPrefix(str, "http") {
								item.Assets["intermediate."+k] = str
							}
						}
					}
				}
			}
		}
		item.PreviewURL = firstURL(item.Assets, "image_url", "pbrImage", "previewUrl", "imageUrl")
		item.PrimaryModel = firstURL(item.Assets, "textureGlb", "glb", "geometryGlb", "obj", "obj_url")
		out = append(out, item)
	}
	return out, total
}

func (s *Server) collectConceptAssets(limit int) ([]AssetItem, int) {
	payload := map[string]interface{}{}
	raw, _ := json.Marshal(payload)
	// Concept works payloads are large (>2KB); parse the untruncated body.
	res := s.callUpstream(r2(), http.MethodPost, ModeConcept, "/api/game3d/general_info/get_works_list", raw)
	full := res.Full
	if full == "" {
		full = res.Raw
	}
	list, total := extractWorksWithTotal(full)
	if total == 0 {
		total = len(list)
	}
	if len(list) > limit {
		list = list[:limit]
	}
	out := make([]AssetItem, 0, len(list))
	for _, w := range list {
		mi, _ := w["modelInfo"].(map[string]interface{})
		item := AssetItem{
			Source:    "concept",
			Assets:    map[string]string{},
			ID:        asString(w["worksId"]),
			Prompt:    asString(w["prompt"]),
			Status:    pipelineStatusName(w["pipelineStatus"], w["status"]),
			Pipeline:  w["worksPipeline"],
			ModelType: asString(w["modelVersion"]),
			CreatedAt: w["createdAt"],
			UpdatedAt: w["updatedAt"],
			Notes:     []string{},
		}
		if p := asString(w["reqPath"]); p != "" {
			item.Notes = append(item.Notes, p)
		}
		if single := asString(w["singleImage"]); strings.HasPrefix(single, "http") {
			item.Assets["singleImage"] = single
		}
		if mi != nil {
			if e := asString(mi["errorMsg"]); e != "" {
				item.Error = e
			}
			if cd, ok := mi["conceptDesignRsp"].(map[string]interface{}); ok {
				if u := asString(cd["imageUrl"]); strings.HasPrefix(u, "http") {
					item.Assets["conceptImage"] = u
				}
				if views, ok := cd["viewsData"].(map[string]interface{}); ok {
					for _, vk := range []string{"frontImageUrl", "backImageUrl", "leftImageUrl", "rightImageUrl"} {
						if u := asString(views[vk]); strings.HasPrefix(u, "http") {
							item.Assets["view_"+strings.TrimSuffix(vk, "ImageUrl")] = u
						}
					}
				}
			}
			for mk, mv := range mi {
				if mm, ok := mv.(map[string]interface{}); ok {
					for uk, uv := range mm {
						if u, ok := uv.(string); ok && strings.HasPrefix(u, "http") {
							key := mk + "." + uk
							if _, exists := item.Assets[key]; !exists {
								item.Assets[key] = u
							}
						}
					}
				}
			}
		}
		item.PreviewURL = firstURL(item.Assets,
			"conceptImage", "conceptDesignRsp.imageUrl", "geometryGenerationRsp.imageUrl",
			"singleImage", "view_front")
		item.PrimaryModel = firstURL(item.Assets,
			"geometryGenerationRsp.glbUrl", "geometryGenerationRsp.fbxUrl",
			"modelInfo.glbUrl", "glbUrl")
		for k, v := range item.Assets {
			if strings.Contains(k, "glbUrl") || strings.Contains(k, "fbxUrl") {
				if item.PrimaryModel == "" {
					item.PrimaryModel = v
				}
			}
		}
		out = append(out, item)
	}
	return out, total
}

func extractCreationsWithTotal(raw string, body interface{}) ([]map[string]interface{}, int) {
	try := func(s string) ([]map[string]interface{}, int) {
		var env struct {
			TotalCount int                      `json:"totalCount"`
			Creations  []map[string]interface{} `json:"creations"`
		}
		if err := json.Unmarshal([]byte(s), &env); err == nil && (len(env.Creations) > 0 || env.TotalCount > 0) {
			return env.Creations, env.TotalCount
		}
		return nil, 0
	}
	if list, total := try(raw); len(list) > 0 || total > 0 {
		return list, total
	}
	if b, ok := body.(map[string]interface{}); ok {
		if arr, ok := b["creations"].([]interface{}); ok {
			out := make([]map[string]interface{}, 0, len(arr))
			for _, it := range arr {
				if m, ok := it.(map[string]interface{}); ok {
					out = append(out, m)
				}
			}
			tc, _ := b["totalCount"].(float64)
			return out, int(tc)
		}
	}
	if raw != "" {
		idx := 0
		for {
			start := strings.Index(raw[idx:], `{"creations"`)
			if start < 0 {
				start = strings.Index(raw[idx:], `{"totalCount"`)
				if start < 0 {
					break
				}
			}
			start += idx
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
			if list, total := try(raw[start:end]); len(list) > 0 || total > 0 {
				return list, total
			}
			idx = end
		}
	}
	return nil, 0
}

func extractWorksWithTotal(raw string) ([]map[string]interface{}, int) {
	list := extractWorksList(raw)
	total := 0
	// try parse total from success doc
	idx := 0
	for {
		start := strings.Index(raw[idx:], `"total"`)
		if start < 0 {
			break
		}
		snip := raw[idx+start:]
		if i := strings.Index(snip, ":"); i >= 0 {
			rest := strings.TrimSpace(snip[i+1:])
			var n int
			j := 0
			for j < len(rest) && rest[j] >= '0' && rest[j] <= '9' {
				j++
			}
			if j > 0 && json.Unmarshal([]byte(rest[:j]), &n) == nil && n > total {
				total = n
			}
		}
		idx += start + 7
	}
	return list, total
}

// r2 returns a bare *http.Request for callUpstream signature compatibility.
func r2() *http.Request {
	req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1/", nil)
	return req
}

func asString(v interface{}) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		b, _ := json.Marshal(v)
		s := string(b)
		if len(s) >= 2 && s[0] == '"' {
			var out string
			_ = json.Unmarshal(b, &out)
			return out
		}
		return s
	}
}

func firstURL(m map[string]string, keys ...string) string {
	for _, k := range keys {
		if u := m[k]; u != "" {
			return u
		}
	}
	pref := []string{"image", "Image", "png", "preview", "cover"}
	for _, p := range pref {
		for k, v := range m {
			if strings.Contains(k, p) && v != "" {
				return v
			}
		}
	}
	return ""
}

func pipelineStatusName(ps, st interface{}) string {
	switch asString(ps) {
	case "0", "1":
		return "processing"
	case "2":
		if e := asString(st); e == "3" || e == "failed" {
			return "failed"
		}
		return "success"
	case "3":
		return "failed"
	}
	if s := asString(st); s != "" {
		switch s {
		case "0", "1":
			return "processing"
		case "2":
			return "success"
		case "3":
			return "failed"
		}
		return s
	}
	return "unknown"
}
