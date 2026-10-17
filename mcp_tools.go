package main

// The MCP tool catalogue: everything the local proxy exposes, wrapped with
// compact projections so an LLM client never receives the raw 200KB board.

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// mcpToolSet builds the full catalogue against a data path.
func mcpToolSet(call func(string, string, []byte) (*localResult, error)) []mcpTool {
	// callJSON dispatches a local request and unwraps the {ok,data,error} envelope.
	callJSON := func(method, path string, body []byte) (map[string]interface{}, error) {
		res, err := call(method, path, body)
		if err != nil {
			return nil, err
		}
		env, err := decodeEnvelopeJSON(res.Body)
		if err != nil {
			return nil, fmt.Errorf("local %s %s returned HTTP %d, unparsable body: %v", method, path, res.Status, err)
		}
		if ok, _ := env["ok"].(bool); !ok {
			msg, _ := env["error"].(string)
			if msg == "" {
				msg = fmt.Sprintf("local %s %s failed with HTTP %d", method, path, res.Status)
			}
			return nil, fmt.Errorf("%s", msg)
		}
		data, _ := env["data"].(map[string]interface{})
		if data == nil {
			data = map[string]interface{}{}
		}
		return data, nil
	}

	return []mcpTool{
		{
			Name: "hunyuan_health",
			Description: "反代服务健康检查：上游地址、默认模式、端口、签名键。" +
				"不含任何账号与会话信息（登录态与积分请用 Web 控制台查看）。",
			InputSchema: obj(map[string]interface{}{}),
			Handler: func(_ map[string]interface{}) (interface{}, error) {
				d, err := callJSON("GET", "/api/health", nil)
				if err != nil {
					return nil, err
				}
				// Deliberately excludes cookieCount / sessionLike: those describe
				// the account (whether a session is loaded), not the service.
				return pick(d, "service", "status", "upstream", "mode", "port", "signKey"), nil
			},
		},
		{
			Name: "hunyuan_set_mode",
			Description: "切换**默认**上游模式：main（主站）或 concept（概念站）。" +
				"注意：生成类工具（hunyuan_generate_concept / hunyuan_generate_main）各自绑定平台，**不需要先切 mode**；" +
				"本工具只影响未显式指定 mode 的裸代理调用（如 /game3d/* 或 /api/3d/* 透传）。" +
				"两站积分池独立：main=remainQuota，concept=remainingTimes，互不通用。",
			InputSchema: obj(map[string]interface{}{
				"mode": enumProp("目标模式", "main", "main", "concept"),
			}, "mode"),
			Handler: func(args map[string]interface{}) (interface{}, error) {
				mode := argEnum(args, "mode", "", []string{"main", "concept"})
				if mode == "" {
					return nil, fmt.Errorf("mode must be main or concept")
				}
				return callJSON("POST", "/api/mode?mode="+mode, nil)
			},
		},
		{
			Name: "hunyuan_generate_concept",
			Description: "【消耗积分】概念站（concept）文生3D：提交提示词。kind=design 只出设计图，kind=geometry 直接出几何模型（GLB），" +
				"kind=both 两步都跑。异步任务，立即返回；随后用 hunyuan_assets(source=concept) 轮询 status/progress，完成后取 primaryModelUrl。" +
				"**何时用 concept**：需要 3D 模型文件本身、要精细控制（面数/风格/版本）、要后续纹理与管线加工（UV/分割/绑定/动作）时。扣 concept 积分池。" +
				"与 hunyuan_generate_main 的区别：主站偏「一次给几个候选挑一个」的快速出图，concept 偏「可控的单件生产」。",
			InputSchema: obj(map[string]interface{}{
				"prompt": strProp("中文或英文提示词，越具体越好（主体+风格+材质）"),
				"kind":   enumProp("生成类型", "both", "design", "geometry", "both"),
				"style": enumProp("设计图风格（只影响设计图外观，不影响几何质量；不传则 hand_drawn）", "hand_drawn",
					"hand_drawn", "q_version", "steampunk", "futuristic", "pixel", "low-poly"),
				"modelVersion": enumProp("模型版本", "v3.1", "v3.0", "v3.1", "v3.5-preview"),
				"faceCount":    intProp("目标面数（默认 500000）"),
				"apose":        boolProp("是否生成 A-pose（默认 false）"),
			}, "prompt"),
			Handler: func(args map[string]interface{}) (interface{}, error) {
				prompt := argString(args, "prompt")
				if prompt == "" {
					return nil, fmt.Errorf("prompt is required")
				}
				payload := map[string]interface{}{"prompt": prompt}
				style, serr := argEnumStrict(args, "style", "hand_drawn",
					[]string{"hand_drawn", "q_version", "steampunk", "futuristic", "pixel", "low-poly"})
				if serr != nil {
					return nil, serr
				}
				putIfSet(payload, "style", style)
				putIfSet(payload, "modelVersion", argString(args, "modelVersion"))
				if n := argInt(args, "faceCount", 0); n > 0 {
					payload["faceCount"] = n
				}
				if v, ok := args["apose"]; ok {
					payload["apose"] = toBool(v)
				}
				kind := argEnum(args, "kind", "both", []string{"design", "geometry", "both"})
				switch kind {
				case "design":
					payload["mode"] = "concept-design"
				case "geometry":
					payload["mode"] = "text-geometry"
				default:
					payload["mode"] = "both"
				}
				body, _ := jsonMarshal(payload)
				d, err := callJSON("POST", "/api/text3d/generate", body)
				if err != nil {
					return nil, err
				}
				return submitSummary(d, map[string]interface{}{
					"kind": kind,
					"hint": "已提交。" + kind + "；用 hunyuan_assets(source=concept) 查看 status/progress，success 后取 primaryModelUrl。",
				}), nil
			},
		},
		{
			Name: "hunyuan_generate_main",
			Description: "【消耗积分】主站（main）文生3D：提交提示词，上游**固定一次生成 4 个候选**（count=1 会被上游拒绝）。" +
				"异步，随后用 hunyuan_main_creations 查询结果。" +
				"**何时用 main**：只想快速拿到几个不同方案挑一个、不需要后续纹理/管线加工时。扣 main 积分池（与 concept 独立）。" +
				"要可交付的单件模型（含纹理、可控制面数）请改用 hunyuan_generate_concept。",
			InputSchema: obj(map[string]interface{}{
				"prompt": strProp("中文或英文提示词"),
				"style": enumProp("贴图风格（不传则上游默认）", "hand_drawn",
					"hand_drawn", "cartoon", "stone", "celadon", "chinese", "cyberpunk"),
				"modelVersion": enumProp("模型版本", "v3.1", "v3.1", "v3.5"),
				"faceCount":    intProp("目标面数（默认 500000）"),
			}, "prompt"),
			Handler: func(args map[string]interface{}) (interface{}, error) {
				prompt := argString(args, "prompt")
				if prompt == "" {
					return nil, fmt.Errorf("prompt is required")
				}
				payload := map[string]interface{}{"prompt": prompt}
				mstyle, mserr := argEnumStrict(args, "style", "",
					[]string{"cartoon", "stone", "celadon", "chinese", "cyberpunk", "hand_drawn"})
				if mserr != nil {
					return nil, mserr
				}
				putIfSet(payload, "style", mstyle)
				putIfSet(payload, "modelVersion", argString(args, "modelVersion"))
				if n := argInt(args, "faceCount", 0); n > 0 {
					payload["faceCount"] = n
				}
				body, _ := jsonMarshal(payload)
				d, err := callJSON("POST", "/api/text3d/main", body)
				if err != nil {
					return nil, err
				}
				return submitSummary(d, map[string]interface{}{
					"hint": "已提交（上游固定生成 4 个候选）；用 hunyuan_main_creations 查看任务与产物链接。",
				}), nil
			},
		},
		{
			Name: "hunyuan_texture",
			Description: "【消耗积分】给已有几何模型（白模）加纹理，产出带 PBR 贴图的成品 GLB。" +
				"几何生成只出白模（materials=0），**这一步才是成品**。传 worksId 即可（自动取该任务的 fbxUrl+imageUrl）。" +
				"异步：返回新的 worksId，用 hunyuan_works 轮询，完成后取 texturePaintingRsp.glbUrl。" +
				"验收：下载 GLB 后 materials 必须 > 0（白模是 0）。",
			InputSchema: obj(map[string]interface{}{
				"worksId":    strProp("几何任务的 worksId（推荐；自动推导 fbxUrl/imageUrl）"),
				"fbxUrl":     strProp("或显式给几何 fbxUrl"),
				"imageUrl":   strProp("或显式给渲染图 imageUrl（method=image 时需要）"),
				"method":     enumProp("纹理路径：text=文生纹理（提示词直接生成贴图）, image=图生纹理, views=多视图", "image", "image", "views", "text"),
				"prompt":     strProp("method=text 时的贴图描述，例如「木质台灯，暖色灯罩」"),
				"viewsImage": strProp("method=views 时的四视图拼接图"),
				"keepUv":     boolProp("保留几何已有 UV（默认 false，重新展开）"),
			}),
			Handler: func(args map[string]interface{}) (interface{}, error) {
				payload := map[string]interface{}{}
				for _, k := range []string{"worksId", "fbxUrl", "imageUrl", "viewsImage", "method", "prompt"} {
					putIfSet(payload, k, argString(args, k))
				}
				if v, ok := args["keepUv"]; ok {
					payload["keepUv"] = toBool(v)
				}
				if argString(args, "worksId") == "" && argString(args, "fbxUrl") == "" {
					return nil, fmt.Errorf("provide worksId (or fbxUrl + imageUrl)")
				}
				body, _ := jsonMarshal(payload)
				d, err := callJSON("POST", "/api/text3d/texture", body)
				if err != nil {
					return nil, err
				}
				return d, nil
			},
		},
		{
			Name: "hunyuan_assets",
			Description: "资源看板：列出主站+概念站的生成任务（含中间产物 URL）。这是查询异步任务进度与最终模型下载链接的主要工具。" +
				"source=concept 时对应概念站任务，status=processing|success 可过滤；返回已裁剪，只保留关键字段。",
			InputSchema: obj(map[string]interface{}{
				"source": enumProp("数据来源", "all", "all", "main", "concept"),
				"status": enumProp("任务状态过滤", "all", "all", "processing", "success"),
				"limit":  intProp("返回条数（默认 10，最大 50）"),
				"offset": intProp("分页偏移（默认 0）"),
			}),
			Handler: func(args map[string]interface{}) (interface{}, error) {
				source := argEnum(args, "source", "all", []string{"all", "main", "concept"})
				status := argEnum(args, "status", "all", []string{"all", "processing", "success"})
				limit := clamp(argInt(args, "limit", 10), 1, 50)
				offset := argInt(args, "offset", 0)
				if offset < 0 {
					offset = 0
				}
				path := fmt.Sprintf("/api/text3d/assets?source=%s&status=%s&limit=%d&offset=%d", source, status, limit, offset)
				d, err := callJSON("GET", path, nil)
				if err != nil {
					return nil, err
				}
				items, _ := d["items"].([]interface{})
				compact := make([]map[string]interface{}, 0, len(items))
				for _, raw := range items {
					it, ok := raw.(map[string]interface{})
					if !ok {
						continue
					}
					row := pick(it, "source", "id", "title", "status", "progress", "previewUrl", "primaryModelUrl", "error")
					if a, ok := it["assets"].(map[string]interface{}); ok {
						row["assetCount"] = len(a)
					}
					compact = append(compact, row)
				}
				return map[string]interface{}{
					"count":      len(compact),
					"totalCount": d["totalCount"],
					"offset":     offset,
					"items":      compact,
					"hint":       "status=processing 的任务继续等待；status=success 后取 primaryModelUrl 下载模型。",
				}, nil
			},
		},
		{
			Name: "hunyuan_works",
			Description: "概念站任务列表（get_works_list 原始视图）：pipelineStatus、产物 URL（singleImage/conceptImage/views/geometry）。" +
				"hunyuan_assets 更方便；需要原始 pipeline 细节时用这个。",
			InputSchema: obj(map[string]interface{}{
				"pipeline": strProp("worksPipeline 过滤，仅支持 1 或 2（留空为全部）"),
				"worksId":  strProp("只看某个 worksId（留空为全部）"),
				"limit":    intProp("返回条数（默认 10，最大 50）"),
			}),
			Handler: func(args map[string]interface{}) (interface{}, error) {
				path := "/api/text3d/works"
				if p := strings.TrimSpace(argString(args, "pipeline")); p != "" {
					path += "?pipeline=" + p
				}
				d, err := callJSON("GET", path, nil)
				if err != nil {
					return nil, err
				}
				// /api/text3d/works returns the array under "works";
				// keep "list" as a tolerant fallback.
				list, _ := d["works"].([]interface{})
				if list == nil {
					list, _ = d["list"].([]interface{})
				}
				limit := clamp(argInt(args, "limit", 10), 1, 50)
				wantID := strings.TrimSpace(argString(args, "worksId"))
				out := make([]map[string]interface{}, 0, limit)
				for _, raw := range list {
					it, ok := raw.(map[string]interface{})
					if !ok {
						continue
					}
					if wantID != "" && argString(it, "worksId") != wantID {
						continue
					}
					out = append(out, pick(it, "worksId", "worksPipeline", "pipelineStatus", "status",
						"prompt", "model", "modelVersion", "errorMsg", "resultUrls", "createdAt"))
					if len(out) >= limit {
						break
					}
				}
				total := d["count"]
				if total == nil {
					total = len(list)
				}
				return map[string]interface{}{
					"count":       len(out),
					"totalOnPage": total,
					"items":       out,
					"filtered":    wantID != "",
					"hint":        "按 worksId 过滤时，未命中该页则 count=0；可先用 hunyuan_assets 取到任务 id。",
				}, nil
			},
		},
		{
			Name:        "hunyuan_main_creations",
			Description: "主站 creations/list 任务列表：返回主站生成的候选模型（含模型/贴图下载链接）。用于 hunyuan_generate_main 之后查询结果。",
			InputSchema: obj(map[string]interface{}{
				"limit":  intProp("返回条数（默认 10，最大 50）"),
				"offset": intProp("分页偏移（默认 0）"),
			}),
			Handler: func(args map[string]interface{}) (interface{}, error) {
				limit := clamp(argInt(args, "limit", 10), 1, 50)
				offset := argInt(args, "offset", 0)
				if offset < 0 {
					offset = 0
				}
				body, _ := jsonMarshal(map[string]interface{}{"limit": limit, "offset": offset})
				d, err := callJSON("POST", "/api/text3d/main/list", body)
				if err != nil {
					return nil, err
				}
				// upstream body arrives as a JSON string; try to surface its list.
				if s, ok := d["body"].(string); ok && s != "" {
					if inner, err := decodeEnvelopeJSON([]byte(s)); err == nil {
						return map[string]interface{}{"status": d["status"], "data": inner}, nil
					}
				}
				return pick(d, "status", "body"), nil
			},
		},
	}
}

// ---- small argument / rendering helpers ----

type prop = map[string]interface{}

// obj builds a JSON Schema object.
func obj(props map[string]interface{}, required ...string) map[string]interface{} {
	schema := map[string]interface{}{
		"type":                 "object",
		"properties":           props,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func strProp(desc string) prop {
	return prop{"type": "string", "description": desc}
}

func intProp(desc string) prop {
	return prop{"type": "integer", "description": desc}
}

func boolProp(desc string) prop {
	return prop{"type": "boolean", "description": desc}
}

func enumProp(desc, def string, values ...string) prop {
	p := prop{"type": "string", "description": desc, "enum": values}
	if def != "" {
		p["default"] = def
	}
	return p
}

func argString(args map[string]interface{}, key string) string {
	if v, ok := args[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func argInt(args map[string]interface{}, key string, def int) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return def
}

// argEnumStrict returns (value, ok). It reports an explicit error for a value
// outside the allowed set instead of silently substituting the default — used
// where a wrong value would quietly change the user's intent (e.g. 风格).
func argEnumStrict(args map[string]interface{}, key, def string, allowed []string) (string, error) {
	v := strings.ToLower(argString(args, key))
	if v == "" {
		return def, nil
	}
	for _, a := range allowed {
		if v == a {
			return v, nil
		}
	}
	return "", fmt.Errorf("%s=%q 不是合法取值，可选：%s", key, v, strings.Join(allowed, ", "))
}

func argEnum(args map[string]interface{}, key, def string, allowed []string) string {
	v := strings.ToLower(argString(args, key))
	if v == "" {
		return def
	}
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	return def
}

func toBool(v interface{}) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return strings.EqualFold(t, "true") || t == "1" || strings.EqualFold(t, "yes")
	case float64:
		return t != 0
	}
	return false
}

func putIfSet(m map[string]interface{}, key, val string) {
	if val != "" {
		m[key] = val
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// pick copies the listed keys (present ones only) into a new map.
func pick(m map[string]interface{}, keys ...string) map[string]interface{} {
	out := make(map[string]interface{}, len(keys))
	for _, k := range keys {
		if v, ok := m[k]; ok {
			out[k] = v
		}
	}
	return out
}

// submitSummary projects a submit response down to what a caller needs next:
// the task identifiers plus the upstream acknowledgement, dropping the
// credit/entitlement blobs the local envelope piggybacks on every call.
func submitSummary(d map[string]interface{}, extra map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range extra {
		out[k] = v
	}
	// Submission metadata echoed back by the proxy.
	for _, k := range []string{"requestId", "submittedAt", "mode", "modelVersion", "style", "faceCount", "prompt"} {
		if v, ok := d[k]; ok {
			out[k] = v
		}
	}
	// Upstream stage results: keep the ack, hoist worksId/requestId.
	stages := []string{"text2geometry", "text2image", "generations", "creations"}
	found := false
	for _, stage := range stages {
		st, ok := d[stage].(map[string]interface{})
		if !ok {
			continue
		}
		found = true
		entry := map[string]interface{}{}
		if v, ok := st["error"]; ok && v != "" && v != nil {
			entry["error"] = v
		}
		for _, k := range []string{"status", "ok", "method", "path"} {
			if v, ok := st[k]; ok {
				entry[k] = v
			}
		}
		if inner, ok := st["body"].(map[string]interface{}); ok {
			for _, k := range []string{"worksId", "requestId", "worksPipeline", "pipelineStatus", "status", "model", "errNo", "errMsg"} {
				if v, ok := inner[k]; ok && v != "" && v != nil {
					entry[k] = v
				}
			}
		}
		out[stage] = entry
	}
	if !found {
		// Fall back to the whole payload minus the noise, so an unexpected
		// upstream shape is still visible instead of silently dropped.
		for k, v := range d {
			if strings.HasPrefix(k, "credit_") || k == "hint" {
				continue
			}
			if _, exists := out[k]; !exists {
				out[k] = v
			}
		}
	}
	return out
}

// withHint attaches a guidance string for the model.
func withHint(m map[string]interface{}, hint string) map[string]interface{} {
	if m == nil {
		m = map[string]interface{}{}
	}
	m["hint"] = hint
	return m
}

// jsonMarshal is a tiny indirection so tool code reads cleanly.
func jsonMarshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}
