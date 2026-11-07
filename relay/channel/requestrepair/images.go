package requestrepair

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// RepairImages replaces rejected image inputs with text while preserving tool structure.
func RepairImages(body []byte, status int, errorBody []byte) ([]byte, bool, error) {
	if !gjson.ValidBytes(body) || !IsImageFallbackStatus(status) || !isUnsupportedImageError(status, errorBody) {
		return body, false, nil
	}
	return stripRequestImages(body)
}

// IsImageFallbackStatus reports whether an upstream status can reject image capabilities.
func IsImageFallbackStatus(status int) bool {
	switch status {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity, http.StatusNotImplemented:
		return true
	default:
		return false
	}
}

func isUnsupportedImageError(status int, body []byte) bool {
	message := errorDescription(body)
	if strings.Contains(message, "no endpoints found that support image input") {
		return true
	}
	if status == http.StatusNotFound {
		return false
	}
	if containsAny(message, "only support text", "only supports text", "text-only", "text only") {
		return true
	}
	if strings.Contains(message, "content.type 参数非法") && strings.Contains(message, "取值范围 ['text']") {
		return true
	}
	if !containsAny(message, "image", "vision", "multimodal", "multi-modal", "modality", "modalities", "media", "attachment", "图片", "图像", "视觉", "多模态") {
		return false
	}
	if containsAny(message, "image format", "image size", "image dimensions", "image resolution", "mime type", "base64") {
		return false
	}
	return containsAny(message, "unsupported", "not supported", "not support", "doesn't support", "does not support", "don't support", "do not support", "cannot support", "can't support", "cannot handle", "can't handle", "cannot process", "can't process", "not allowed", "not accept", "does not accept", "doesn't accept", "unable to process", "invalid content type", "invalid message content", "unknown variant", "unknown content type", "unrecognized content type", "不支持", "不接受", "不允许", "无法处理")
}

func containsAny(text string, markers ...string) bool {
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func stripRequestImages(body []byte) ([]byte, bool, error) {
	out := body
	changed := false
	for _, root := range []string{"messages", "input"} {
		items := gjson.GetBytes(out, root)
		if !items.IsArray() {
			continue
		}
		for i, item := range items.Array() {
			path := root + "." + strconv.Itoa(i)
			// Responses permits input_image items directly in the input array.
			if root == "input" && item.Get("type").String() == "input_image" {
				patched, err := sjson.SetRawBytes(out, path, imagePlaceholder(item, true))
				if err != nil {
					return body, false, err
				}
				out, changed = patched, true
				continue
			}
			field := "content"
			if root == "input" && item.Get("type").String() == "function_call_output" {
				field = "output"
			}
			content, contentChanged, err := stripImageContent(item.Get(field), root == "input")
			if field == "output" || item.Get("role").String() == "tool" {
				content, contentChanged, err = stripToolImages(item.Get(field), root == "input")
			}
			if err != nil {
				return body, false, err
			}
			if contentChanged {
				out, err = sjson.SetRawBytes(out, path+"."+field, content)
				if err != nil {
					return body, false, err
				}
				changed = true
			}
		}
	}
	return out, changed, nil
}

func stripImageContent(content gjson.Result, responses bool) ([]byte, bool, error) {
	if !content.IsArray() {
		return nil, false, nil
	}
	parts := make([]json.RawMessage, 0, len(content.Array()))
	changed := false
	for _, block := range content.Array() {
		raw := []byte(block.Raw)
		switch block.Get("type").String() {
		case "image", "image_url", "input_image":
			raw = imagePlaceholder(block, responses)
			changed = true
		case "tool_result":
			nested, nestedChanged, err := stripToolImages(block.Get("content"), false)
			if err != nil {
				return nil, false, err
			}
			if nestedChanged {
				raw, err = sjson.SetRawBytes(raw, "content", nested)
				if err != nil {
					return nil, false, err
				}
				changed = true
			}
		}
		parts = append(parts, raw)
	}
	if !changed {
		return nil, false, nil
	}
	out, err := json.Marshal(parts)
	return out, err == nil, err
}

func stripToolImages(value gjson.Result, responses bool) ([]byte, bool, error) {
	if value.IsArray() {
		return stripImageContent(value, responses)
	}
	if value.Type == gjson.String {
		text := value.String()
		if !gjson.Valid(text) {
			return nil, false, nil
		}
		raw, changed, err := stripToolImages(gjson.Parse(text), responses)
		if err != nil || !changed {
			return nil, false, err
		}
		encoded, err := json.Marshal(string(raw))
		return encoded, err == nil, err
	}
	if !value.IsObject() {
		return nil, false, nil
	}
	switch value.Get("type").String() {
	case "image", "image_url", "input_image":
		return imagePlaceholder(value, responses), true, nil
	}
	out := []byte(value.Raw)
	changed := false
	// MCP wrappers contain output content under these fields. Tool arguments and
	// arbitrary business properties are deliberately outside this traversal.
	for _, field := range []string{"content", "result"} {
		raw, nestedChanged, err := stripToolImages(value.Get(field), responses)
		if err != nil {
			return nil, false, err
		}
		if nestedChanged {
			out, err = sjson.SetRawBytes(out, field, raw)
			if err != nil {
				return nil, false, err
			}
			changed = true
		}
	}
	return out, changed, nil
}

func imagePlaceholder(block gjson.Result, responses bool) []byte {
	textType := "text"
	if responses {
		textType = "input_text"
	}
	placeholder := []byte(`{"type":"` + textType + `","text":"[Unsupported Image]"}`)
	if cache := block.Get("cache_control"); cache.Exists() {
		placeholder = append(placeholder[:len(placeholder)-1], []byte(`,"cache_control":`)...)
		placeholder = append(placeholder, cache.Raw...)
		placeholder = append(placeholder, '}')
	}
	return placeholder
}
