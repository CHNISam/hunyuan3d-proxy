package main

// Texture stage: turns a white geometry model into a finished, PBR-textured GLB.
//
// The pipeline is two-stage and asynchronous:
//
//	geometry (text2geometry)  → white model, materials=0
//	texture  (image2texture)  → textured model, materials=1 + 3 PBR maps
//
// Three texture paths, verified against the live upstream:
//
//	image2texture_v31   fbx + rendered image      → works (5/5)
//	views2texture       fbx + 4-view sheet        → works (1/1)
//	texture_edit_local  fbx + worksId + editType  → works (text-to-texture too)
//
// IMPORTANT: text-to-texture does NOT go through text2texture_v31. That
// endpoint runs hunyuan-3d-texture-preprocess-v1.0, which fails with HTTP 400
// for every request (7/7 observed). The real text-to-texture path, captured
// from the SPA, is texture_edit_local with editType=1, which runs
// hunyuan-3d-texture-edit-text-v1.0 and succeeds.

import (
	"net/http"
	"strings"
)

// TextureRequest is the local request body for POST /api/text3d/texture.
type TextureRequest struct {
	// WorksID of the geometry task to texture. When set, fbxUrl/imageUrl are
	// taken from that task automatically.
	WorksID string `json:"worksId,omitempty"`
	// Explicit inputs, used when they are not derived from WorksID.
	FbxURL   string `json:"fbxUrl,omitempty"`
	ImageURL string `json:"imageUrl,omitempty"`
	// View images for the multi-view path (views2texture).
	ViewsImage string `json:"viewsImage,omitempty"`
	// KeepUv keeps the geometry's existing UV layout instead of re-unwrapping.
	KeepUv bool `json:"keepUv,omitempty"`
	// Method selects the texture path: "image" (default) | "views" | "text".
	Method string `json:"method,omitempty"`
	// Prompt drives the text-to-texture path (method=text).
	Prompt string `json:"prompt,omitempty"`
	// LocalNames / SegmentationInfo are part of the SPA's texture_edit_local
	// payload; empty means "whole model".
	LocalNames       []string `json:"localNames,omitempty"`
	SegmentationInfo string   `json:"segmentationInfo,omitempty"`
	// ModelVersion defaults to v3.1.
	ModelVersion string `json:"modelVersion,omitempty"`
}

// textureStage is one upstream texture call.
type textureStage struct {
	Key    string
	Path   string
	Result map[string]interface{}
}

// HandleText3DTexture POST /api/text3d/texture
//
// Submits a texture job for an existing geometry task (or explicit URLs) and
// returns the upstream acknowledgement. Poll the returned worksId with
// /api/text3d/works to collect texturePaintingRsp.* outputs.
func (s *Server) HandleText3DTexture(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	var req TextureRequest
	if err := readJSONBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	fbx := strings.TrimSpace(req.FbxURL)
	img := strings.TrimSpace(req.ImageURL)
	views := strings.TrimSpace(req.ViewsImage)
	dependsOn := strings.TrimSpace(req.WorksID)

	method := strings.ToLower(strings.TrimSpace(req.Method))
	if method == "" {
		method = "image"
	}

	// Resolve the inputs from an existing worksId when not given.
	// method=text needs a UV-unwrapped mesh, so it prefers the fbx produced by
	// a previous texture stage over the raw geometry mesh.
	if dependsOn != "" && (fbx == "" || (img == "" && views == "")) {
		derived := s.textureInputsFromWorks(r, dependsOn, method == "text")
		if fbx == "" {
			fbx = derived.FbxURL
		}
		if img == "" {
			img = derived.ImageURL
		}
		if views == "" {
			views = derived.ViewsImage
		}
	}

	if fbx == "" {
		writeErr(w, http.StatusBadRequest,
			"fbxUrl is required (or pass worksId of a finished geometry task to derive it)")
		return
	}

	version := strings.TrimSpace(req.ModelVersion)
	if version == "" {
		version = "v3.1"
	}

	path := "/api/game3d/texture_painting/image2texture_v31"
	switch method {
	case "image":
		if img == "" {
			writeErr(w, http.StatusBadRequest,
				"imageUrl is required for method=image (a rendered preview drives the texture)")
			return
		}
	case "views":
		path = "/api/game3d/texture_painting/views2texture"
		if views == "" {
			writeErr(w, http.StatusBadRequest, "viewsImage is required for method=views")
			return
		}
	case "text":
		// Text-to-texture uses texture_edit_local with editType=1, NOT
		// text2texture_v31 (whose texture-preprocess step always fails).
		//
		// It needs a UV-unwrapped mesh: pass a geometry task's own fbx and the
		// upstream runs but produces no texturePaintingRsp. The working input is
		// the fbx from a previous texture stage (texturePaintingRsp.fbxUrl),
		// which is what textureInputsFromWorks prefers for this method.
		path = "/api/game3d/texture_painting/texture_edit_local"
		if strings.TrimSpace(req.Prompt) == "" {
			writeErr(w, http.StatusBadRequest,
				"prompt is required for method=text (the text describes the desired material)")
			return
		}
	default:
		writeErr(w, http.StatusBadRequest, "unknown method: "+method+" (use image|views|text)")
		return
	}

	requestID := "go" + randomNonce(12)
	payload := map[string]interface{}{
		"fbxUrl":          fbx,
		"dependOnWorksId": dependsOn,
		"worksType":       2,
		"requestId":       requestID,
		"modelVersion":    version,
		"keepUv":          req.KeepUv,
	}
	switch method {
	case "image":
		payload["imageUrl"] = img
	case "views":
		payload["viewsImage"] = views
	case "text":
		// Shape captured from the live SPA.
		payload["worksId"] = dependsOn
		payload["editType"] = 1 // TEXT_2_TEXTURE
		payload["prompt"] = strings.TrimSpace(req.Prompt)
		payload["localNames"] = req.LocalNames
		if payload["localNames"] == nil {
			payload["localNames"] = []string{}
		}
		payload["segmentationInfo"] = req.SegmentationInfo
	}

	raw, code, err := textureCall(s, r, path, payload)
	res := map[string]interface{}{
		"requestId":    requestID,
		"method":       method,
		"modelVersion": version,
		"keepUv":       req.KeepUv,
		"upstream":     path,
		"status":       code,
		"body":         raw,
	}
	if err != nil {
		res["error"] = err.Error()
	}
	// Surface the new worksId so the caller can poll it directly.
	if body, ok := raw.(map[string]interface{}); ok {
		if wid, ok := body["worksId"].(string); ok && wid != "" {
			res["worksId"] = wid
			res["hint"] = "轮询 GET /api/text3d/works，完成后取 resultUrls.texturePaintingRsp.glbUrl（成品），" +
				"先看 glb 是否 materials>0 再判定成功"
		}
	}
	writeOK(w, res)
}

// textureCall is the seam used to submit a texture job. Tests replace it to
// assert request shaping without spending credits.
var textureCall = func(s *Server, r *http.Request, path string, payload map[string]interface{}) (interface{}, int, error) {
	return s.callUpstreamJSON(r, http.MethodPost, ModeConcept, path, payload)
}

// textureInputsFromWorksCall fetches the works list; also replaceable for tests.
var textureInputsFromWorksCall = func(s *Server, r *http.Request) string {
	return s.callUpstreamRaw(r, http.MethodPost, ModeConcept,
		"/api/game3d/general_info/get_works_list", []byte("{}"))
}

// textureInputs mirrors the fields a geometry task exposes for texturing.
type textureInputs struct {
	FbxURL     string
	ImageURL   string
	ViewsImage string
}

// textureInputsFromWorks pulls the outputs out of the works list.
//
// preferTextured picks the fbx produced by a previous texture stage
// (texturePaintingRsp.fbxUrl, already UV-unwrapped) instead of the raw
// geometry mesh. method=text requires the unwrapped one: the raw geometry fbx
// runs without error but yields no texture outputs.
func (s *Server) textureInputsFromWorks(r *http.Request, worksID string, preferTextured bool) textureInputs {
	out := textureInputs{}
	full := textureInputsFromWorksCall(s, r)
	list := extractWorksList(full)
	for _, w := range list {
		if asString(w["worksId"]) != worksID {
			continue
		}
		mi, _ := w["modelInfo"].(map[string]interface{})
		pick := func(m map[string]interface{}, keys ...string) string {
			for _, k := range keys {
				if v := asString(m[k]); v != "" {
					return v
				}
			}
			return ""
		}
		if tp, ok := mi["texturePaintingRsp"].(map[string]interface{}); ok {
			// Preferred for method=text: this fbx carries a UV layout.
			if preferTextured {
				out.FbxURL = pick(tp, "fbxUrl", "FbxUrl")
			}
			if out.ImageURL == "" {
				out.ImageURL = pick(tp, "imageUrl", "ImageUrl")
			}
		}
		if out.FbxURL == "" {
			if geo, ok := mi["geometryGenerationRsp"].(map[string]interface{}); ok {
				out.FbxURL = pick(geo, "fbxUrl", "FbxUrl")
				out.ImageURL = pick(geo, "imageUrl", "ImageUrl")
			}
		}
		if out.ImageURL == "" {
			out.ImageURL = pick(w, "singleImage")
		}
		// Four-view sheet, when the task produced one.
		if cd, ok := mi["conceptDesignRsp"].(map[string]interface{}); ok {
			if vd, ok := cd["viewsData"].(map[string]interface{}); ok {
				out.ViewsImage = pick(vd, "viewsImage", "viewsImageUrl")
			}
		}
		break
	}
	return out
}
