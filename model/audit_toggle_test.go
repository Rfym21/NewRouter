package model

import (
	"errors"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"new-router/common"
	"new-router/setting/operation_setting"
)

func setupAuditToggleTest(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&Option{}, &AuditLog{}, &Log{}, &User{}))
	oldDB, oldLogDB := DB, LOG_DB
	oldMainType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	oldEnabled, oldConsume, oldRedis := operation_setting.IsAuditLogEnabled(), common.LogConsumeEnabled, common.RedisEnabled
	preserveLogRangeOptionState(t)
	DB, LOG_DB = db, db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	common.LogConsumeEnabled, common.RedisEnabled = true, false
	operation_setting.SetAuditLogEnabled(true)
	t.Cleanup(func() {
		DB, LOG_DB = oldDB, oldLogDB
		common.SetMainDatabaseType(oldMainType)
		common.SetLogDatabaseType(oldLogType)
		common.LogConsumeEnabled, common.RedisEnabled = oldConsume, oldRedis
		operation_setting.SetAuditLogEnabled(oldEnabled)
		assert.NoError(t, sqlDB.Close())
	})
	return db
}

func TestAuditToggleStopsAllCategoriesAndPreservesUsageAndHistory(t *testing.T) {
	db := setupAuditToggleTest(t)
	categories := []string{AuditCategoryLogin, AuditCategorySecurity, AuditCategoryOperation, AuditCategoryAccessToken}
	for _, category := range categories {
		RecordAuditLog(nil, AuditLog{UserId: 1, Username: "root", ActorRole: common.RoleRootUser, Category: category, Success: true})
	}
	assertAuditCount := func(want int64) {
		t.Helper()
		var count int64
		require.NoError(t, db.Model(&AuditLog{}).Count(&count).Error)
		assert.Equal(t, want, count)
	}
	assertAuditCount(4)
	require.NoError(t, UpdateOption(operation_setting.AuditLogEnabledOptionKey, "false"))
	for _, category := range categories {
		RecordAuditLog(nil, AuditLog{UserId: 1, Username: "root", ActorRole: common.RoleRootUser, Category: category})
	}
	RecordLoginLog(1, common.RoleRootUser, "root", "login", "", "login", nil, AuditOther{})
	RecordOperationAuditLog(1, common.RoleRootUser, "security", "", "security", nil, nil, nil)
	RecordOperationAuditLog(1, common.RoleRootUser, "operation", "", "operation", nil, &AuditAdminInfo{AdminRole: common.RoleRootUser}, nil)
	RecordLogWithAdminInfo(1, LogTypeManage, "legacy operation", nil, nil)
	assertAuditCount(4)

	require.NoError(t, db.Create(&User{Id: 1, Username: "root", Role: common.RoleRootUser}).Error)
	RecordLog(1, LogTypeConsume, common.NewMessage("usage"))
	var count int64
	require.NoError(t, db.Model(&Log{}).Where("type = ?", LogTypeConsume).Count(&count).Error)
	assert.EqualValues(t, 1, count)
	logs, total, err := GetAuditLogs(AuditLogFilter{}, 0, 10, common.RoleRootUser)
	require.NoError(t, err)
	assert.EqualValues(t, 4, total)
	assert.Len(t, logs, 4)

	require.NoError(t, UpdateOption(operation_setting.AuditLogEnabledOptionKey, "true"))
	RecordLoginLog(1, common.RoleRootUser, "root", "login again", "", "login", nil, AuditOther{})
	assertAuditCount(5)
}

func TestAuditToggleDefaultsPersistenceAndHotReload(t *testing.T) {
	db := setupAuditToggleTest(t)
	InitOptionMap()
	assert.True(t, operation_setting.IsAuditLogEnabled())
	assert.Equal(t, "true", common.OptionMap[operation_setting.AuditLogEnabledOptionKey])
	require.NoError(t, UpdateOption(operation_setting.AuditLogEnabledOptionKey, "false"))
	assert.False(t, operation_setting.IsAuditLogEnabled())
	assert.Equal(t, "false", requireOptionValue(t, db, operation_setting.AuditLogEnabledOptionKey))
	operation_setting.SetAuditLogEnabled(true) // Simulate a fresh process before persisted options load.
	InitOptionMap()
	assert.False(t, operation_setting.IsAuditLogEnabled())
	assert.Equal(t, "false", common.OptionMap[operation_setting.AuditLogEnabledOptionKey])
	require.NoError(t, db.Model(&Option{}).Where("key = ?", operation_setting.AuditLogEnabledOptionKey).Update("value", "true").Error)
	loadOptionsFromDatabase()
	assert.True(t, operation_setting.IsAuditLogEnabled())
	assert.Equal(t, "true", common.OptionMap[operation_setting.AuditLogEnabledOptionKey])
	for _, value := range []string{"", "0", "FALSE", "invalid"} {
		require.Error(t, UpdateOption(operation_setting.AuditLogEnabledOptionKey, value))
		require.Error(t, updateOptionMap(operation_setting.AuditLogEnabledOptionKey, value))
		assert.True(t, operation_setting.IsAuditLogEnabled())
		assert.Equal(t, "true", common.OptionMap[operation_setting.AuditLogEnabledOptionKey])
		assert.Equal(t, "true", requireOptionValue(t, db, operation_setting.AuditLogEnabledOptionKey))
	}
	const callback = "audit-toggle-failed-save"
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) { tx.AddError(errors.New("save failed")) }))
	t.Cleanup(func() { db.Callback().Update().Remove(callback) })
	require.Error(t, UpdateOption(operation_setting.AuditLogEnabledOptionKey, "false"))
	assert.True(t, operation_setting.IsAuditLogEnabled())
	assert.Equal(t, "true", common.OptionMap[operation_setting.AuditLogEnabledOptionKey])
	assert.Equal(t, "true", requireOptionValue(t, db, operation_setting.AuditLogEnabledOptionKey))
}
