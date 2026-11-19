package request_plugin_setting

import (
	"encoding/json"
	"slices"
	"sync"

	"new-router/common"
	"new-router/setting/config"
)

// InstancesOption is the option key of the instance list.
const InstancesOption = "request_plugin_setting.instances"

// SharedConfigsOption is the option key of the plugin shared configs.
const SharedConfigsOption = "request_plugin_setting.shared_configs"

// Instance is one configured use of a request plugin. Global instances run
// on every request, group instances on the requests of their groups, and a
// channel runs the instances its request_plugins setting lists. Models
// narrows any of them to requests for those models; empty means every
// model. Whether a failure stops the request is the plugin's meta.optional;
// the required member of older saved lists is dropped on load.
//
// A plugin scope instance runs where its plugin's meta.scope says instead,
// on every channel and model that fits it, new ones included, but only where
// the scope an administrator confirmed holds the request too: a plugin
// update that narrows its scope applies at once, and one that widens it
// reaches the new requests once an administrator confirms it again.
type Instance struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Plugin  string   `json:"plugin"`
	Enabled bool     `json:"enabled"`
	Global  bool     `json:"global"`
	Groups  []string `json:"groups"`
	Models  []string `json:"models"`
	// Channels limits the instance to those channel ids; empty runs it on
	// every channel, like Global and Groups. A request whose channel is not
	// settled yet does not run the instance until a later stage.
	Channels []int `json:"channels,omitempty"`
	// ExcludeChannels removes channels from the instance's other scopes.
	ExcludeChannels []int `json:"exclude_channels,omitempty"`
	// ManagedBy marks an instance a host policy owns; the instance editor
	// must not edit it. Empty means an administrator created it.
	ManagedBy string          `json:"managed_by,omitempty"`
	Config    json.RawMessage `json:"config"`
	// PluginScope is the confirmed scope of a plugin scope instance; nil for
	// an instance placed by hand.
	PluginScope *Scope `json:"plugin_scope,omitempty"`
}

// Scope is a plugin's scope as an administrator confirmed it: its models
// and channel types; an empty list leaves that side open.
type Scope struct {
	Models       []string `json:"models"`
	ChannelTypes []int    `json:"channel_types"`
}

// Instances is the stored instance list. Every load replaces the whole list
// under instancesMu, so a request keeps a consistent list it read once.
type Instances []Instance

var instancesMu sync.RWMutex

func (list *Instances) UnmarshalJSON(data []byte) error {
	var decoded []Instance
	if err := common.Unmarshal(data, &decoded); err != nil {
		return err
	}
	// Lists saved before models existed read as every model.
	for index := range decoded {
		if decoded[index].Models == nil {
			decoded[index].Models = []string{}
		}
	}
	instancesMu.Lock()
	*list = decoded
	instancesMu.Unlock()
	return nil
}

// SharedConfigs is the config one plugin shares with every instance of it,
// stored by plugin key. An instance reads it as ctx.shared; the instance
// config can override what it needs.
type SharedConfigs map[string]json.RawMessage

var sharedConfigsMu sync.RWMutex

func (configs *SharedConfigs) UnmarshalJSON(data []byte) error {
	var decoded map[string]json.RawMessage
	if err := common.Unmarshal(data, &decoded); err != nil {
		return err
	}
	sharedConfigsMu.Lock()
	*configs = decoded
	sharedConfigsMu.Unlock()
	return nil
}

type RequestPluginSetting struct {
	Instances     Instances     `json:"instances"`
	SharedConfigs SharedConfigs `json:"shared_configs"`
}

var requestPluginSetting = RequestPluginSetting{Instances: Instances{}, SharedConfigs: SharedConfigs{}}

func init() {
	config.GlobalConfig.Register("request_plugin_setting", &requestPluginSetting)
}

// GetInstances returns the instance list in option order. The list is shared
// and must not be changed.
func GetInstances() []Instance {
	instancesMu.RLock()
	defer instancesMu.RUnlock()
	return requestPluginSetting.Instances
}

// GetSharedConfig returns the stored shared config of plugin, nil when it has
// none. The caller may change the result.
func GetSharedConfig(plugin string) json.RawMessage {
	sharedConfigsMu.RLock()
	defer sharedConfigsMu.RUnlock()
	return slices.Clone(requestPluginSetting.SharedConfigs[plugin])
}

// GetSharedConfigs returns the shared configs in a copy the caller may change.
func GetSharedConfigs() map[string]json.RawMessage {
	sharedConfigsMu.RLock()
	defer sharedConfigsMu.RUnlock()
	configs := make(map[string]json.RawMessage, len(requestPluginSetting.SharedConfigs))
	for key, value := range requestPluginSetting.SharedConfigs {
		configs[key] = slices.Clone(value)
	}
	return configs
}

// Covers reports whether the scope holds a request for model, whose name
// without its @ modifiers is base, sent through a channel of channelType; 0
// before a channel is selected, when only a scope without channel types
// holds it.
func (s *Scope) Covers(model, base string, channelType int) bool {
	if len(s.Models) > 0 && !slices.Contains(s.Models, base) && !slices.Contains(s.Models, model) {
		return false
	}
	return len(s.ChannelTypes) == 0 || channelType > 0 && slices.Contains(s.ChannelTypes, channelType)
}

// Equal reports whether the scope holds the same models and channel types,
// in any order.
func (s *Scope) Equal(models []string, channelTypes []int) bool {
	return sameItems(s.Models, models) && sameItems(s.ChannelTypes, channelTypes)
}

func sameItems[T comparable](a, b []T) bool {
	if len(a) != len(b) {
		return false
	}
	for _, item := range a {
		if !slices.Contains(b, item) {
			return false
		}
	}
	return true
}

// Clone returns a copy with empty lists, never nil ones.
func (s *Scope) Clone() *Scope {
	return &Scope{Models: append([]string{}, s.Models...), ChannelTypes: append([]int{}, s.ChannelTypes...)}
}
