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

func setupAnonymizationPolicyTestDB(t *testing.T) {
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
		// A rebuild leaves its instances in the shared setting; reset them so
		// the next test starts from an empty list.
		require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{request_plugin_setting.InstancesOption: "[]"}))
		model.DB = previousDB
		common.OptionMap = previousOptions
		_ = sqlDB.Close()
	})
}

func TestRebuildAnonymizationPolicyProjectsChannels(t *testing.T) {
	setupAnonymizationPolicyTestDB(t)
	previousEnabled := setting.AnonymizationEnabled
	previousStable := setting.AnonymizationStableSurrogates
	t.Cleanup(func() {
		setting.AnonymizationEnabled = previousEnabled
		setting.AnonymizationStableSurrogates = previousStable
	})
	setting.AnonymizationEnabled = true
	setting.AnonymizationStableSurrogates = true

	inherit := &model.Channel{Name: "inherit", Key: "k", Status: common.ChannelStatusEnabled}
	require.NoError(t, model.DB.Create(inherit).Error)

	off := false
	disabled := &model.Channel{Name: "disabled", Key: "k", Status: common.ChannelStatusEnabled}
	disabled.SetSetting(dto.ChannelSettings{AnonymizationEnabled: &off})
	require.NoError(t, model.DB.Create(disabled).Error)

	require.NoError(t, RebuildAnonymizationPolicy())

	byID := map[string]request_plugin_setting.Instance{}
	for _, instance := range request_plugin_setting.GetInstances() {
		byID[instance.ID] = instance
	}
	global, ok := byID[anonymizationPolicyGlobalID]
	require.True(t, ok)
	assert.True(t, global.Enabled)
	assert.Contains(t, global.ExcludeChannels, disabled.Id)
	assert.NotContains(t, global.ExcludeChannels, inherit.Id)

	policy, err := ManagedAnonymizationPolicyFor(inherit.Id)
	require.NoError(t, err)
	assert.True(t, policy.Managed)
	assert.True(t, policy.EnabledByDefault)
	assert.True(t, policy.StableSurrogates)
	assert.NotNil(t, policy.Rules)

	offPolicy, err := ManagedAnonymizationPolicyFor(disabled.Id)
	require.NoError(t, err)
	assert.True(t, offPolicy.Managed)
	assert.False(t, offPolicy.EnabledByDefault)
}

func TestRebuildAnonymizationPolicyChannelForceOn(t *testing.T) {
	setupAnonymizationPolicyTestDB(t)
	previousEnabled := setting.AnonymizationEnabled
	t.Cleanup(func() { setting.AnonymizationEnabled = previousEnabled })
	setting.AnonymizationEnabled = false

	enabled := true
	channel := &model.Channel{Name: "forced", Key: "k", Status: common.ChannelStatusEnabled}
	channel.SetSetting(dto.ChannelSettings{AnonymizationEnabled: &enabled})
	require.NoError(t, model.DB.Create(channel).Error)

	require.NoError(t, RebuildAnonymizationPolicy())

	byID := map[string]request_plugin_setting.Instance{}
	for _, instance := range request_plugin_setting.GetInstances() {
		byID[instance.ID] = instance
	}
	channelInstance, ok := byID[fmt.Sprintf(anonymizationPolicyChannelID, channel.Id)]
	require.True(t, ok)
	assert.Equal(t, []int{channel.Id}, channelInstance.Channels)

	policy, err := ManagedAnonymizationPolicyFor(channel.Id)
	require.NoError(t, err)
	assert.True(t, policy.Managed)
	assert.True(t, policy.EnabledByDefault)
}

func TestAnonymizationPolicyFallsBackWithoutInstances(t *testing.T) {
	setupAnonymizationPolicyTestDB(t)
	require.NoError(t, model.UpdateOption(request_plugin_setting.InstancesOption, "[]"))

	policy, err := ManagedAnonymizationPolicyFor(7)
	require.NoError(t, err)
	assert.False(t, policy.Managed)
}
