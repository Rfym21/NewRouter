package service

import (
	"encoding/json"
	"fmt"

	"new-router/common"
	"new-router/model"
	"new-router/setting"
	"new-router/setting/request_plugin_setting"
)

// ManagedBySensitivePolicy marks the request-plugin instances the
// sensitive-word policy owns. The instance editor must leave them alone.
const ManagedBySensitivePolicy = "sensitive-policy"

const (
	sensitivePolicyGlobalID  = "rp_sensitive_global"
	sensitivePolicyChannelID = "rp_sensitive_ch%d"
	sensitivePolicyPlugin    = "request-sensitive"
)

// sensitivePolicyConfig is the request-sensitive plugin config the policy
// writes: the effective word list of the instance's scope.
type sensitivePolicyConfig struct {
	Keywords              string `json:"keywords"`
	KeywordsCaseSensitive bool   `json:"keywordsCaseSensitive"`
	ReturnWords           bool   `json:"returnWords"`
	Message               string `json:"message"`
	Rules                 []any  `json:"rules"`
}

// HasManagedSensitivePolicy reports whether the policy instances exist. The
// old Go word check runs only while they do not, so a rollout can be rolled
// back by removing them.
func HasManagedSensitivePolicy() bool {
	for _, instance := range request_plugin_setting.GetInstances() {
		if instance.ManagedBy == ManagedBySensitivePolicy {
			return true
		}
	}
	return false
}

// RebuildSensitivePolicy projects the global sensitive-word options and the
// channel overrides into request-plugin instances. The global word list stays
// in one global instance; a channel that replaces the list, or enables the
// check while the global switch is off, gets its own instance, and the global
// one excludes it so no word is checked twice.
func RebuildSensitivePolicy() error {
	globalEnabled := setting.CheckSensitiveEnabled && setting.CheckSensitiveOnPromptEnabled
	globalWords := setting.SensitiveWords
	returnWords := common.SensitiveErrorReturnWordsEnabled

	channels, err := model.GetAllChannels(0, 0, true, false)
	if err != nil {
		return fmt.Errorf("load channels for sensitive policy: %w", err)
	}

	var managed []request_plugin_setting.Instance
	excluded := make([]int, 0, len(channels))
	for _, channel := range channels {
		if channel == nil {
			continue
		}
		settings := channel.GetSetting()
		words := settings.EffectiveSensitiveWords(globalWords)
		if !settings.ShouldCheckSensitive(globalEnabled) || len(words) == 0 {
			excluded = append(excluded, channel.Id)
			continue
		}
		if globalEnabled && (settings.SensitiveMode == "" || settings.SensitiveMode == "inherit") {
			continue
		}
		// The channel's own instance replaces the global one for it.
		excluded = append(excluded, channel.Id)
		config, err := marshalSensitivePolicyConfig(words, returnWords)
		if err != nil {
			return err
		}
		managed = append(managed, request_plugin_setting.Instance{
			ID:        fmt.Sprintf(sensitivePolicyChannelID, channel.Id),
			Name:      truncateInstanceName("敏感词 · " + channel.Name),
			Plugin:    sensitivePolicyPlugin,
			Enabled:   true,
			Global:    false,
			Groups:    []string{},
			Models:    []string{},
			Channels:  []int{channel.Id},
			ManagedBy: ManagedBySensitivePolicy,
			Config:    config,
		})
	}

	globalConfig, err := marshalSensitivePolicyConfig(globalWords, returnWords)
	if err != nil {
		return err
	}
	global := request_plugin_setting.Instance{
		ID:              sensitivePolicyGlobalID,
		Name:            "敏感词",
		Plugin:          sensitivePolicyPlugin,
		Enabled:         globalEnabled && len(globalWords) > 0,
		Global:          true,
		Groups:          []string{},
		Models:          []string{},
		ExcludeChannels: excluded,
		ManagedBy:       ManagedBySensitivePolicy,
		Config:          globalConfig,
	}

	instances := make([]request_plugin_setting.Instance, 0, len(managed)+1)
	for _, instance := range request_plugin_setting.GetInstances() {
		if instance.ManagedBy == ManagedBySensitivePolicy {
			continue
		}
		instances = append(instances, instance)
	}
	instances = append(instances, global)
	instances = append(instances, managed...)

	encoded, err := common.Marshal(instances)
	if err != nil {
		return fmt.Errorf("encode sensitive policy instances: %w", err)
	}
	if err := model.UpdateOption(request_plugin_setting.InstancesOption, string(encoded)); err != nil {
		return fmt.Errorf("save sensitive policy instances: %w", err)
	}
	return nil
}

// MigrateSensitivePolicy builds the policy instances once on an installation
// that still has the old options. It keeps those options so removing the
// managed instances restores the old path.
func MigrateSensitivePolicy() error {
	if HasManagedSensitivePolicy() {
		return nil
	}
	if !setting.CheckSensitiveEnabled && len(setting.SensitiveWords) == 0 {
		return nil
	}
	return RebuildSensitivePolicy()
}

func marshalSensitivePolicyConfig(words []string, returnWords bool) (json.RawMessage, error) {
	keywords := ""
	for index, word := range words {
		if index > 0 {
			keywords += "\n"
		}
		keywords += word
	}
	encoded, err := common.Marshal(sensitivePolicyConfig{
		Keywords:              keywords,
		KeywordsCaseSensitive: false,
		ReturnWords:           returnWords,
		Rules:                 []any{},
	})
	return json.RawMessage(encoded), err
}

// truncateInstanceName keeps a managed instance name within the host limit.
func truncateInstanceName(name string) string {
	const limit = 64
	runes := []rune(name)
	if len(runes) <= limit {
		return name
	}
	return string(runes[:limit])
}
