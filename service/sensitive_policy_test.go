package service

import (
	"fmt"
	"testing"

	"new-router/common"
	"new-router/model"
	"new-router/relaykit/dto"
	"new-router/setting"
	"new-router/setting/config"
	"new-router/setting/request_plugin_setting"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupSensitivePolicyTestDB(t *testing.T) {
	t.Helper()
	previousDB := model.DB
	previousOptions := common.OptionMap
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Option{}, &model.SystemTask{}))
	model.DB = db
	common.OptionMap = map[string]string{}
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{request_plugin_setting.InstancesOption: "[]"}))
		model.DB = previousDB
		common.OptionMap = previousOptions
		_ = sqlDB.Close()
	})
}

func withSensitiveOptions(t *testing.T, words []string, enabled bool) {
	t.Helper()
	previousWords, previousEnabled := setting.SensitiveWords, setting.CheckSensitiveEnabled
	previousPrompt := setting.CheckSensitiveOnPromptEnabled
	previousReturn := common.SensitiveErrorReturnWordsEnabled
	setting.SensitiveWords, setting.CheckSensitiveEnabled = words, enabled
	setting.CheckSensitiveOnPromptEnabled = enabled
	t.Cleanup(func() {
		setting.SensitiveWords = previousWords
		setting.CheckSensitiveEnabled = previousEnabled
		setting.CheckSensitiveOnPromptEnabled = previousPrompt
		common.SensitiveErrorReturnWordsEnabled = previousReturn
	})
}

func policyKeywords(t *testing.T, instance request_plugin_setting.Instance) string {
	t.Helper()
	var config sensitivePolicyConfig
	require.NoError(t, common.Unmarshal(instance.Config, &config))
	return config.Keywords
}

func TestRebuildSensitivePolicyProjectsChannels(t *testing.T) {
	setupSensitivePolicyTestDB(t)
	withSensitiveOptions(t, []string{"secret", "hidden"}, true)
	common.SensitiveErrorReturnWordsEnabled = true

	inherit := &model.Channel{Name: "inherit", Key: "k", Status: common.ChannelStatusEnabled}
	require.NoError(t, model.DB.Create(inherit).Error)

	override := &model.Channel{Name: "override", Key: "k", Status: common.ChannelStatusEnabled}
	override.SetSetting(dto.ChannelSettings{SensitiveMode: "override", SensitiveOverrideWords: []string{"private"}})
	require.NoError(t, model.DB.Create(override).Error)

	disabled := false
	off := &model.Channel{Name: "off", Key: "k", Status: common.ChannelStatusEnabled}
	off.SetSetting(dto.ChannelSettings{SensitiveCheckEnabled: &disabled})
	require.NoError(t, model.DB.Create(off).Error)

	require.NoError(t, RebuildSensitivePolicy())

	byID := map[string]request_plugin_setting.Instance{}
	for _, instance := range request_plugin_setting.GetInstances() {
		byID[instance.ID] = instance
	}

	global, ok := byID[sensitivePolicyGlobalID]
	require.True(t, ok)
	assert.True(t, global.Enabled)
	assert.Equal(t, "secret\nhidden", policyKeywords(t, global))
	// A channel that overrides the list, and one that turns the check off, are
	// excluded from the global instance so no word is checked twice.
	assert.Contains(t, global.ExcludeChannels, override.Id)
	assert.Contains(t, global.ExcludeChannels, off.Id)
	assert.NotContains(t, global.ExcludeChannels, inherit.Id)

	channelInstance, ok := byID[fmt.Sprintf(sensitivePolicyChannelID, override.Id)]
	require.True(t, ok)
	assert.Equal(t, []int{override.Id}, channelInstance.Channels)
	assert.Equal(t, "private", policyKeywords(t, channelInstance))

	_, ok = byID[fmt.Sprintf(sensitivePolicyChannelID, off.Id)]
	assert.False(t, ok, "a disabled channel gets no instance")
}

func TestRebuildSensitivePolicyKeepsAdminInstances(t *testing.T) {
	setupSensitivePolicyTestDB(t)
	withSensitiveOptions(t, []string{"secret"}, true)

	admin := request_plugin_setting.Instance{
		ID: "rp_admin", Name: "Admin", Plugin: "request-filter", Enabled: true, Global: true,
		Groups: []string{}, Models: []string{}, Config: []byte(`{"rules":[]}`),
	}
	encoded, err := common.Marshal([]request_plugin_setting.Instance{admin})
	require.NoError(t, err)
	require.NoError(t, model.UpdateOption(request_plugin_setting.InstancesOption, string(encoded)))

	require.NoError(t, RebuildSensitivePolicy())

	instances := request_plugin_setting.GetInstances()
	require.Len(t, instances, 2)
	assert.Equal(t, "rp_admin", instances[0].ID, "administrator instances keep their place")
	assert.Equal(t, sensitivePolicyGlobalID, instances[1].ID)
}

func TestRebuildSensitivePolicyChannelEnabledWhileGlobalOff(t *testing.T) {
	setupSensitivePolicyTestDB(t)
	withSensitiveOptions(t, []string{"secret"}, false)

	enabled := true
	channel := &model.Channel{Name: "forced", Key: "k", Status: common.ChannelStatusEnabled}
	channel.SetSetting(dto.ChannelSettings{SensitiveCheckEnabled: &enabled})
	require.NoError(t, model.DB.Create(channel).Error)

	require.NoError(t, RebuildSensitivePolicy())

	byID := map[string]request_plugin_setting.Instance{}
	for _, instance := range request_plugin_setting.GetInstances() {
		byID[instance.ID] = instance
	}
	assert.False(t, byID[sensitivePolicyGlobalID].Enabled)
	channelInstance, ok := byID[fmt.Sprintf(sensitivePolicyChannelID, channel.Id)]
	require.True(t, ok)
	assert.True(t, channelInstance.Enabled)
	assert.Equal(t, []int{channel.Id}, channelInstance.Channels)
	assert.Equal(t, "secret", policyKeywords(t, channelInstance))
}
