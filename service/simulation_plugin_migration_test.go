package service

import (
	"testing"

	"new-router/common"
	"new-router/model"
	"new-router/setting/config"
	"new-router/setting/request_plugin_setting"

	_ "new-router/plugins"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMigrateSimulationPlugins(t *testing.T) {
	previousDB := model.DB
	previousOptions := common.OptionMap
	previousMaster := common.IsMasterNode
	previousCache := common.MemoryCacheEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Option{}, &model.SystemTask{}))
	model.DB = db
	common.OptionMap = map[string]string{}
	common.IsMasterNode = true
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		model.DB = previousDB
		common.OptionMap = previousOptions
		common.IsMasterNode = previousMaster
		common.MemoryCacheEnabled = previousCache
		_ = config.GlobalConfig.LoadFromDB(map[string]string{
			request_plugin_setting.InstancesOption:     "[]",
			request_plugin_setting.SharedConfigsOption: "{}",
		})
		_ = sqlDB.Close()
	})
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		request_plugin_setting.InstancesOption:     "[]",
		request_plugin_setting.SharedConfigsOption: "{}",
	}))

	channel := &model.Channel{
		Id:            1,
		Type:          14,
		Name:          "Claude",
		Key:           "key",
		Models:        "claude-sonnet-4-5",
		Group:         "default",
		OtherSettings: `{"claude_code_simulation_enabled":true,"claude_code_simulation":{"headers":{"user-agent":"claude-cli/1.0","content-type":"application/json","connection":"keep-alive"},"metadata":{"device_id":"device","account_uuid":"","session_id":"session"},"system":[{"type":"text","text":"hi"}]},"codex_simulation_enabled":true,"codex_simulation":{"metadata":{"session_id":"codex-session"}}}`,
	}
	require.NoError(t, model.DB.Create(channel).Error)
	require.NoError(t, MigrateSimulationPlugins())

	var saved model.Channel
	require.NoError(t, model.DB.First(&saved, channel.Id).Error)
	assert.NotContains(t, saved.OtherSettings, "claude_code_simulation")
	assert.NotContains(t, saved.OtherSettings, "codex_simulation")
	setting := saved.GetSetting()
	assert.Contains(t, setting.RequestPlugins, "rp_ccsim_1")
	assert.Contains(t, setting.RequestPlugins, "rp_cxsim_1")

	instances := request_plugin_setting.GetInstances()
	var claude, codex bool
	for _, instance := range instances {
		switch instance.Plugin {
		case "claude-code-simulation":
			claude = instance.ID == "rp_ccsim_1" && instance.Enabled
		case "codex-simulation":
			codex = instance.ID == "rp_cxsim_1" && instance.Enabled
		}
	}
	assert.True(t, claude)
	assert.True(t, codex)
	claudeShared := string(request_plugin_setting.GetSharedConfig("claude-code-simulation"))
	assert.Contains(t, claudeShared, `"device_id":"device"`)
	assert.Contains(t, claudeShared, `"user-agent":"claude-cli/1.0"`)
	assert.NotContains(t, claudeShared, `"content-type"`)
	assert.NotContains(t, claudeShared, `"connection"`)
	assert.Contains(t, string(request_plugin_setting.GetSharedConfig("codex-simulation")), `"session_id":"codex-session"`)

	// A second run keeps the same instances and bindings.
	require.NoError(t, MigrateSimulationPlugins())
	assert.Len(t, request_plugin_setting.GetInstances(), len(instances))
	var again model.Channel
	require.NoError(t, model.DB.First(&again, channel.Id).Error)
	assert.Equal(t, len(setting.RequestPlugins), len(again.GetSetting().RequestPlugins))
}
