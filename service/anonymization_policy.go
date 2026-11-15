package service

import (
	"encoding/json"
	"fmt"
	"slices"
	"sync"

	"new-router/common"
	"new-router/model"
	"new-router/pkg/anonymizer"
	"new-router/setting"
	"new-router/setting/request_plugin_setting"
)

// ManagedByAnonymizationPolicy marks the request-plugin instances the
// anonymization policy owns. The engine itself stays in Go: the instances
// carry the rules and switches, and the response restorers keep their state.
const ManagedByAnonymizationPolicy = "anonymization-policy"

const (
	anonymizationPolicyGlobalID  = "rp_anonymizer_global"
	anonymizationPolicyChannelID = "rp_anonymizer_ch%d"
	anonymizationPolicyPlugin    = "builtin-anonymizer"
)

type anonymizationPolicyConfig struct {
	EnabledByDefault bool   `json:"enabledByDefault"`
	StableSurrogates bool   `json:"stableSurrogates"`
	StrictContext    bool   `json:"strictContext"`
	Rules            string `json:"rules"`
}

// ManagedAnonymizationPolicy is the policy an instance carries for one
// channel. Managed reports that a managed instance decided it; the caller
// then uses it instead of the legacy request-policy options.
type ManagedAnonymizationPolicy struct {
	EnabledByDefault bool
	StableSurrogates bool
	StrictContext    bool
	Rules            *anonymizer.RuleSet
	Managed          bool
}

// rules cache: compiled rule sets are immutable and shared by every session.
var anonymizationRulesCache sync.Map // raw JSON -> *anonymizer.RuleSet

func cachedAnonymizationRules(raw string) (*anonymizer.RuleSet, error) {
	if cached, ok := anonymizationRulesCache.Load(raw); ok {
		return cached.(*anonymizer.RuleSet), nil
	}
	rules, err := setting.ParseAnonymizationRules(raw)
	if err != nil {
		return nil, err
	}
	anonymizationRulesCache.Store(raw, rules)
	return rules, nil
}

// ManagedAnonymizationPolicyFor resolves the managed policy of a channel. A
// channel-scoped instance wins over the global one; a global instance that
// excludes the channel reports the check as off.
func ManagedAnonymizationPolicyFor(channelID int) (ManagedAnonymizationPolicy, error) {
	var global, channelInstance *request_plugin_setting.Instance
	instances := request_plugin_setting.GetInstances()
	for index := range instances {
		instance := &instances[index]
		if instance.ManagedBy != ManagedByAnonymizationPolicy || !instance.Enabled {
			continue
		}
		if len(instance.Channels) > 0 && slices.Contains(instance.Channels, channelID) {
			channelInstance = instance
		}
		if instance.Global {
			global = instance
		}
	}
	picked := channelInstance
	if picked == nil {
		if global == nil {
			return ManagedAnonymizationPolicy{}, nil
		}
		if channelID != 0 && slices.Contains(global.ExcludeChannels, channelID) {
			return ManagedAnonymizationPolicy{Managed: true}, nil
		}
		picked = global
	}
	var config anonymizationPolicyConfig
	if err := common.Unmarshal(picked.Config, &config); err != nil {
		return ManagedAnonymizationPolicy{}, fmt.Errorf("read anonymization policy instance %s: %w", picked.ID, err)
	}
	rawRules := config.Rules
	if rawRules == "" {
		rawRules = "[]"
	}
	rules, err := cachedAnonymizationRules(rawRules)
	if err != nil {
		return ManagedAnonymizationPolicy{}, fmt.Errorf("compile anonymization policy instance %s: %w", picked.ID, err)
	}
	return ManagedAnonymizationPolicy{
		EnabledByDefault: config.EnabledByDefault,
		StableSurrogates: config.StableSurrogates,
		StrictContext:    config.StrictContext,
		Rules:            rules,
		Managed:          true,
	}, nil
}

// HasManagedAnonymizationPolicy reports whether the policy instances exist.
// The legacy options stay in effect for an installation that has none, such as
// one that never migrated.
func HasManagedAnonymizationPolicy() bool {
	for _, instance := range request_plugin_setting.GetInstances() {
		if instance.ManagedBy == ManagedByAnonymizationPolicy {
			return true
		}
	}
	return false
}

// RebuildAnonymizationPolicy projects the anonymization options and the
// channel overrides into request-plugin instances. The global instance
// carries the rule set; a channel that forces anonymization on while the
// global switch is off gets its own instance, and channels that turn it off
// are excluded from the global one.
func RebuildAnonymizationPolicy() error {
	policy := model.CurrentRequestPolicy()
	rules := policy.Options[setting.AnonymizationRulesOptionKey]
	globalConfig := anonymizationPolicyConfig{
		// The switches live in the setting package, which the option loader
		// updates; the rule JSON comes from the policy snapshot.
		EnabledByDefault: setting.AnonymizationEnabled,
		StableSurrogates: setting.AnonymizationStableSurrogates,
		StrictContext:    setting.AnonymizationStrictContextEnabled,
		Rules:            rules,
	}
	if globalConfig.Rules == "" {
		globalConfig.Rules = "[]"
	}

	channels, err := model.GetAllChannels(0, 0, true, false)
	if err != nil {
		return fmt.Errorf("load channels for anonymization policy: %w", err)
	}

	var managed []request_plugin_setting.Instance
	excluded := make([]int, 0, len(channels))
	for _, channel := range channels {
		if channel == nil {
			continue
		}
		enabled := channel.GetSetting().ShouldAnonymize(globalConfig.EnabledByDefault)
		if !enabled {
			excluded = append(excluded, channel.Id)
			continue
		}
		if !globalConfig.EnabledByDefault {
			// The channel forces anonymization on while the global switch is
			// off, so it needs its own instance.
			config, err := common.Marshal(anonymizationPolicyConfig{
				EnabledByDefault: true,
				StableSurrogates: globalConfig.StableSurrogates,
				StrictContext:    globalConfig.StrictContext,
				Rules:            globalConfig.Rules,
			})
			if err != nil {
				return err
			}
			managed = append(managed, request_plugin_setting.Instance{
				ID:        fmt.Sprintf(anonymizationPolicyChannelID, channel.Id),
				Name:      truncateInstanceName("匿名化 · " + channel.Name),
				Plugin:    anonymizationPolicyPlugin,
				Enabled:   true,
				Global:    false,
				Groups:    []string{},
				Models:    []string{},
				Channels:  []int{channel.Id},
				ManagedBy: ManagedByAnonymizationPolicy,
				Config:    json.RawMessage(config),
			})
		}
	}

	globalJSON, err := common.Marshal(globalConfig)
	if err != nil {
		return err
	}
	global := request_plugin_setting.Instance{
		ID:              anonymizationPolicyGlobalID,
		Name:            "匿名化",
		Plugin:          anonymizationPolicyPlugin,
		Enabled:         true,
		Global:          true,
		Groups:          []string{},
		Models:          []string{},
		ExcludeChannels: excluded,
		ManagedBy:       ManagedByAnonymizationPolicy,
		Config:          json.RawMessage(globalJSON),
	}

	instances := make([]request_plugin_setting.Instance, 0, len(managed)+1)
	for _, instance := range request_plugin_setting.GetInstances() {
		if instance.ManagedBy == ManagedByAnonymizationPolicy {
			continue
		}
		instances = append(instances, instance)
	}
	instances = append(instances, global)
	instances = append(instances, managed...)

	encoded, err := common.Marshal(instances)
	if err != nil {
		return fmt.Errorf("encode anonymization policy instances: %w", err)
	}
	if err := model.UpdateOption(request_plugin_setting.InstancesOption, string(encoded)); err != nil {
		return fmt.Errorf("save anonymization policy instances: %w", err)
	}
	return nil
}

// MigrateAnonymizationPolicy builds the policy instances once on an
// installation that still has the legacy options. It runs even while the
// global switch is off: a channel may force anonymization on, and the
// global instance then carries the off default. The legacy options stay, and
// the engine still reads them on a node whose instances are missing.
func MigrateAnonymizationPolicy() error {
	if HasManagedAnonymizationPolicy() {
		return nil
	}
	return RebuildAnonymizationPolicy()
}
