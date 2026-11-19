package service

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"new-router/common"
	"new-router/dto"
	"new-router/model"
	"new-router/pkg/jsplugin"
	"new-router/setting/request_plugin_setting"
)

// The built-in plugins that replace the retired channel-level client
// simulation settings.
const (
	claudeSimulationPlugin = "claude-code-simulation"
	codexSimulationPlugin  = "codex-simulation"
)

// legacySimulationSettings is the part of a channel's other settings the old
// channel-level simulation used. It is read only by this migration; the
// fields are removed from the channel settings DTO.
type legacySimulationSettings struct {
	ClaudeCodeSimulationEnabled bool            `json:"claude_code_simulation_enabled"`
	ClaudeCodeSimulation        json.RawMessage `json:"claude_code_simulation"`
	CodexSimulationEnabled      bool            `json:"codex_simulation_enabled"`
	CodexSimulation             json.RawMessage `json:"codex_simulation"`
}

// MigrateSimulationPlugins moves channel-level Claude Code and Codex
// simulation settings to request plugin instances: an instance per enabled
// channel, the channel's request_plugins binding, and the custom fingerprint
// elements in the plugin-level shared config. The retired keys are removed
// from the channel settings afterwards. The migration is idempotent.
func MigrateSimulationPlugins() error {
	if !common.IsMasterNode {
		return nil
	}
	channels, err := model.GetAllChannels(0, 0, true, true)
	if err != nil {
		return fmt.Errorf("load channels for simulation migration: %w", err)
	}
	instances := append([]request_plugin_setting.Instance{}, request_plugin_setting.GetInstances()...)
	sharedConfigs := request_plugin_setting.GetSharedConfigs()
	if sharedConfigs == nil {
		sharedConfigs = map[string]json.RawMessage{}
	}
	byID := make(map[string]bool, len(instances))
	for _, instance := range instances {
		byID[instance.ID] = true
	}
	changed := false
	for _, channel := range channels {
		if channel == nil || strings.TrimSpace(channel.OtherSettings) == "" {
			continue
		}
		var settings map[string]json.RawMessage
		if err := common.UnmarshalJsonStr(channel.OtherSettings, &settings); err != nil {
			continue
		}
		var legacy legacySimulationSettings
		if err := common.UnmarshalJsonStr(channel.OtherSettings, &legacy); err != nil {
			continue
		}
		bindings := channelSimulationBindings(channel)
		migrated := false
		for _, item := range []struct {
			plugin  string
			enabled bool
			raw     json.RawMessage
			prefix  string
		}{
			{claudeSimulationPlugin, legacy.ClaudeCodeSimulationEnabled, legacy.ClaudeCodeSimulation, "ccsim"},
			{codexSimulationPlugin, legacy.CodexSimulationEnabled, legacy.CodexSimulation, "cxsim"},
		} {
			if !item.enabled {
				continue
			}
			if _, ok := jsplugin.DefaultRegistry.GetIgnoringMasterSwitch(item.plugin); !ok {
				common.SysError(common.LogText("simulation migration: plugin %s is not installed; channel %d keeps its old settings", item.plugin, channel.Id))
				continue
			}
			id := "rp_" + item.prefix + "_" + strconv.Itoa(channel.Id)
			if !byID[id] {
				instances = append(instances, request_plugin_setting.Instance{
					ID:      id,
					Name:    simulationInstanceName(item.plugin, channel),
					Plugin:  item.plugin,
					Enabled: true,
					Groups:  []string{},
					Models:  []string{},
					Config:  json.RawMessage("{}"),
				})
				byID[id] = true
			}
			if !containsString(bindings, id) {
				bindings = append(bindings, id)
			}
			if shared, err := simulationSharedConfig(item.raw); err != nil {
				common.SysError(common.LogText("simulation migration: channel %d has an unreadable %s config: %s", channel.Id, item.plugin, err.Error()))
			} else if len(shared) > 0 {
				if existing := sharedConfigs[item.plugin]; len(existing) == 0 {
					sharedConfigs[item.plugin] = shared
				} else if string(existing) != string(shared) {
					common.SysLog(common.LogText("simulation migration: channel %d has a different %s shared config; the first one is kept", channel.Id, item.plugin))
				}
			}
			migrated = true
		}
		if !migrated {
			continue
		}
		delete(settings, "claude_code_simulation_enabled")
		delete(settings, "claude_code_simulation")
		delete(settings, "codex_simulation_enabled")
		delete(settings, "codex_simulation")
		otherSettings, err := common.Marshal(settings)
		if err != nil {
			return fmt.Errorf("encode channel %d settings: %w", channel.Id, err)
		}
		channelSetting, err := channelSettingJSON(channel, bindings)
		if err != nil {
			return fmt.Errorf("encode channel %d setting: %w", channel.Id, err)
		}
		if err := model.DB.Model(&model.Channel{}).Where("id = ?", channel.Id).Updates(map[string]any{
			"settings": string(otherSettings),
			"setting":  channelSetting,
		}).Error; err != nil {
			return fmt.Errorf("save channel %d simulation migration: %w", channel.Id, err)
		}
		changed = true
	}
	if !changed {
		return nil
	}
	encodedInstances, err := common.Marshal(instances)
	if err != nil {
		return err
	}
	encodedShared, err := common.Marshal(sharedConfigs)
	if err != nil {
		return err
	}
	if err := model.UpdateOptionsBulk(map[string]string{
		request_plugin_setting.InstancesOption:     string(encodedInstances),
		request_plugin_setting.SharedConfigsOption: string(encodedShared),
	}); err != nil {
		return fmt.Errorf("save simulation plugin instances: %w", err)
	}
	if common.MemoryCacheEnabled {
		model.InitChannelCache()
	}
	model.BumpSyncTopic(model.SyncTopicChannels)
	common.SysLog(common.LogText("simulation migration: moved channel client simulation to request plugins"))
	return nil
}

// simulationInstanceName builds an instance name within the 64-character
// limit the request plugin settings enforce.
func simulationInstanceName(plugin string, channel *model.Channel) string {
	label := "Claude Code simulation"
	if plugin == codexSimulationPlugin {
		label = "Codex simulation"
	}
	name := label + ": " + channel.Name
	if utf8.RuneCountInString(name) > 64 {
		runes := []rune(name)
		name = string(runes[:64])
	}
	return name
}

// channelSimulationBindings returns the channel's request plugin instance
// bindings, from its setting column.
func channelSimulationBindings(channel *model.Channel) []string {
	if channel == nil || channel.Setting == nil || strings.TrimSpace(*channel.Setting) == "" {
		return nil
	}
	var setting dto.ChannelSettings
	if err := common.UnmarshalJsonStr(*channel.Setting, &setting); err != nil {
		return nil
	}
	return append([]string{}, setting.RequestPlugins...)
}

// channelSettingJSON returns the channel setting with the bindings written,
// preserving every other setting.
func channelSettingJSON(channel *model.Channel, bindings []string) (string, error) {
	setting := dto.ChannelSettings{}
	if channel != nil && channel.Setting != nil && strings.TrimSpace(*channel.Setting) != "" {
		if err := common.UnmarshalJsonStr(*channel.Setting, &setting); err != nil {
			return "", err
		}
	}
	setting.RequestPlugins = bindings
	encoded, err := common.Marshal(setting)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// simulationSharedConfig keeps the configurable fingerprint elements of an
// old channel-level config as a plugin shared config. The plugin schema
// applies the defaults of the members the old config did not set.
func simulationSharedConfig(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return nil, nil
	}
	var source map[string]json.RawMessage
	if err := common.Unmarshal(raw, &source); err != nil {
		return nil, err
	}
	shared := map[string]json.RawMessage{}
	for _, key := range []string{"headers", "metadata", "system"} {
		if value, ok := source[key]; ok && len(value) > 0 && strings.TrimSpace(string(value)) != "null" {
			if key == "headers" {
				value = simulationSafeHeaders(value)
			}
			shared[key] = value
		}
	}
	if len(shared) == 0 {
		return nil, nil
	}
	return common.Marshal(shared)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// simulationSafeHeaders drops the headers a request processing plugin may not
// set, so a migrated shared config does not carry values with no effect.
func simulationSafeHeaders(raw json.RawMessage) json.RawMessage {
	var headers map[string]string
	if err := common.Unmarshal(raw, &headers); err != nil {
		return raw
	}
	for name := range headers {
		if simulationProtectedHeader(name) {
			delete(headers, name)
		}
	}
	encoded, err := common.Marshal(headers)
	if err != nil {
		return raw
	}
	return encoded
}

func simulationProtectedHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "authorization", "proxy-authorization", "cookie", "host", "content-length",
		"content-type", "content-encoding", "connection", "keep-alive",
		"proxy-connection", "transfer-encoding", "upgrade",
		"x-api-key", "api-key", "x-goog-api-key":
		return true
	default:
		return false
	}
}
