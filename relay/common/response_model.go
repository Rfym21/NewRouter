package common

import (
	"bytes"
	"strings"

	rootcommon "new-router/common"
)

var responseModelFieldLiteral = []byte(`"model"`)

// ClientResponseModelName returns the model name that should be exposed to clients.
// When UnifyRequestResponseModelNameEnabled is set, the client-requested model name
// (OriginModelName) is returned so responses echo the requested model rather than the
// upstream/mapped name; otherwise the caller-provided fallback (typically the upstream
// name) is preserved.
func (info *RelayInfo) ClientResponseModelName(fallback string) string {
	if info == nil {
		return fallback
	}
	if info.isResponseModelNameRewriteEnabled() && info.OriginModelName != "" {
		return info.OriginModelName
	}
	if fallback != "" {
		return fallback
	}
	if info.ChannelMeta != nil && info.UpstreamModelName != "" {
		return info.UpstreamModelName
	}
	return info.OriginModelName
}

func (info *RelayInfo) isResponseModelNameRewriteEnabled() bool {
	return info != nil &&
		info.ChannelMeta != nil &&
		info.ChannelMeta.ChannelSetting.UnifyRequestResponseModelNameEnabled
}

// ApplyResponseModelNameBytes rewrites JSON response model fields when the channel setting is enabled.
func ApplyResponseModelNameBytes(data []byte, info *RelayInfo) []byte {
	modelName := info.ClientResponseModelName("")
	if !shouldRewriteResponseModelName(info, modelName) || len(data) == 0 {
		return data
	}
	if !bytes.Contains(data, responseModelFieldLiteral) {
		return data
	}

	var payload any
	if err := rootcommon.Unmarshal(data, &payload); err != nil {
		return data
	}
	if !rewriteModelFields(payload, modelName) {
		return data
	}
	rewritten, err := rootcommon.Marshal(payload)
	if err != nil {
		return data
	}
	return rewritten
}

// ApplyResponseModelNameString rewrites JSON response model fields when the channel setting is enabled.
func ApplyResponseModelNameString(data string, info *RelayInfo) string {
	if data == "" {
		return data
	}
	rewritten := ApplyResponseModelNameBytes(rootcommon.StringToByteSlice(data), info)
	if len(rewritten) == len(data) && string(rewritten) == data {
		return data
	}
	return string(rewritten)
}

func shouldRewriteResponseModelName(info *RelayInfo, modelName string) bool {
	return info != nil &&
		info.ChannelMeta != nil &&
		info.ChannelMeta.ChannelSetting.UnifyRequestResponseModelNameEnabled &&
		modelName != ""
}

func rewriteModelFields(value any, modelName string) bool {
	switch typed := value.(type) {
	case map[string]any:
		changed := false
		for key, item := range typed {
			if key == "model" {
				if _, ok := item.(string); ok {
					typed[key] = modelName
					changed = true
					continue
				}
			}
			if rewriteModelFields(item, modelName) {
				changed = true
			}
		}
		return changed
	case []any:
		changed := false
		for _, item := range typed {
			if rewriteModelFields(item, modelName) {
				changed = true
			}
		}
		return changed
	default:
		return false
	}
}

// ResponseModel records upstream declarations before response conversion. It is
// diagnostic only: it must never change routing, pricing, or downstream output.
// Only the three names are stored; whether they disagree is computed on demand
// so every consumer applies the current comparison rule to old rows as well.
type ResponseModel struct {
	RequestedModel string `json:"requested_model"`
	UpstreamModel  string `json:"upstream_model"`
	ReturnedModel  string `json:"returned_model"`
}

// matches reports whether an upstream declaration is compatible with the
// requested or upstream model: equal ignoring case, a dated or variant name
// that extends it, or the same name behind a provider path such as
// "deepseek/deepseek-v4.1-flash".
func (r *ResponseModel) matches(model string) bool {
	returned := strings.ToLower(model)
	for _, expected := range []string{r.RequestedModel, r.UpstreamModel} {
		expected = strings.ToLower(expected)
		if expected != "" && (strings.HasPrefix(returned, expected) || strings.HasSuffix(returned, expected)) {
			return true
		}
	}
	return false
}

// Mismatch reports whether the retained upstream declaration disagrees with
// both the requested and upstream models.
func (r *ResponseModel) Mismatch() bool {
	return r != nil && r.ReturnedModel != "" && !r.matches(r.ReturnedModel)
}

// ObserveResponseModel retains the first differing model for inspection, with
// mismatches taking priority over provider-path, prefix, or case-only
// differences. A later matching or empty event cannot erase it. Only observe
// upstream declarations, never models synthesized by a response converter.
func (info *RelayInfo) ObserveResponseModel(model string) {
	if info == nil || strings.TrimSpace(model) == "" {
		return
	}
	if info.ResponseModel == nil {
		info.ResponseModel = &ResponseModel{
			RequestedModel: info.OriginModelName,
			UpstreamModel:  info.GetUpstreamModelName(),
		}
	}
	observation := info.ResponseModel
	if observation.Mismatch() {
		return
	}
	if observation.matches(model) && observation.ReturnedModel != "" &&
		observation.ReturnedModel != observation.RequestedModel && observation.ReturnedModel != observation.UpstreamModel {
		return
	}
	observation.ReturnedModel = model
}
