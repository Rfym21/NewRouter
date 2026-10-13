package jsplugin

import (
	"new-router/common"
	relaycommon "new-router/relay/common"
	"new-router/setting/model_setting"
)

// providerTaskSettings carries the configuration used by the retired native
// adaptors into the corresponding built-in plugin hooks.
func (a *TaskAdaptor) providerTaskSettings(info *relaycommon.RelayInfo, modelName string) map[string]any {
	settings := make(map[string]any)
	if info != nil {
		if userSettings, ok := jsonValue(info.UserSetting).(map[string]any); ok {
			settings = userSettings
		}
	}
	if a.plugin == nil {
		return settings
	}
	switch a.plugin.Meta.Key {
	case "google":
		settings["geminiVersion"] = model_setting.GetGeminiVersionSetting(modelName)
	case "vertex-ai":
		if info != nil && info.HasChannelMeta() {
			var regions map[string]any
			if common.UnmarshalJsonStr(info.ApiVersion, &regions) == nil && regions != nil {
				settings["vertexRegion"] = regions
			} else {
				settings["vertexRegion"] = info.ApiVersion
			}
		}
	}
	return settings
}
