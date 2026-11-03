package model

import (
	"errors"
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"new-router/common"
	"new-router/setting/config"
	"new-router/setting/operation_setting"
)

func preserveLogRangeOptionState(t *testing.T) {
	t.Helper()
	oldDays, oldLegacy := operation_setting.GetLogSetting().MaxRangeDays, common.LogExportMaxDays
	oldOptions := common.OptionMap
	common.OptionMap = maps.Clone(oldOptions)
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	oldPolicy := CurrentRequestPolicy()
	oldConfig := make(map[string]string)
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error { oldConfig[key] = value; return nil }))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(oldConfig))
		operation_setting.GetLogSetting().MaxRangeDays, common.LogExportMaxDays = oldDays, oldLegacy
		common.OptionMap = oldOptions
		requestPolicySnapshot.Store(oldPolicy)
	})
}

func TestLogRangeOptionMigrationKeepsTheStricterLimit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values map[string]string
		want   string
	}{
		{"two different limits", map[string]string{operation_setting.LogMaxRangeDaysOptionKey: "90", legacyLogExportMaxDaysOptionKey: "7"}, "7"},
		{"query is stricter", map[string]string{operation_setting.LogMaxRangeDaysOptionKey: "7", legacyLogExportMaxDaysOptionKey: "90"}, "7"},
		{"only query saved", map[string]string{operation_setting.LogMaxRangeDaysOptionKey: "90"}, "30"},
		{"only export saved", map[string]string{legacyLogExportMaxDaysOptionKey: "90"}, "90"},
		{"equal values stay equal", map[string]string{operation_setting.LogMaxRangeDaysOptionKey: "400", legacyLogExportMaxDaysOptionKey: "400"}, "400"},
		{"zero query means one day", map[string]string{operation_setting.LogMaxRangeDaysOptionKey: "0", legacyLogExportMaxDaysOptionKey: "30"}, "1"},
		{"zero export means one day", map[string]string{legacyLogExportMaxDaysOptionKey: "0"}, "1"},
		{"negative export means one day", map[string]string{operation_setting.LogMaxRangeDaysOptionKey: "30", legacyLogExportMaxDaysOptionKey: "-1"}, "1"},
		{"invalid query means one day", map[string]string{operation_setting.LogMaxRangeDaysOptionKey: "invalid"}, "1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := useFrontendOptionMigrationDB(t)
			for key, value := range tc.values {
				require.NoError(t, db.Create(&Option{Key: key, Value: value}).Error)
			}
			_, err := migrateLogRangeOptions(nil)
			require.NoError(t, err)
			for _, key := range []string{operation_setting.LogMaxRangeDaysOptionKey, legacyLogExportMaxDaysOptionKey} {
				assert.Equal(t, tc.want, requireOptionValue(t, db, key))
			}
			_, err = migrateLogRangeOptions(nil)
			require.NoError(t, err)
			assert.Equal(t, tc.want, requireOptionValue(t, db, operation_setting.LogMaxRangeDaysOptionKey), "reloading must be idempotent")
		})
	}
}

func TestLogRangeOptionAliasesPersistAcrossReloads(t *testing.T) {
	db := useFrontendOptionMigrationDB(t)
	preserveLogRangeOptionState(t)
	for _, key := range []string{operation_setting.LogMaxRangeDaysOptionKey, legacyLogExportMaxDaysOptionKey} {
		require.NoError(t, UpdateOption(key, "90"))
		for _, storedKey := range []string{operation_setting.LogMaxRangeDaysOptionKey, legacyLogExportMaxDaysOptionKey} {
			assert.Equal(t, "90", requireOptionValue(t, db, storedKey))
			assert.Equal(t, "90", common.OptionMap[storedKey])
		}
		assert.Equal(t, 90, operation_setting.GetLogSetting().MaxRangeDays)
		assert.Equal(t, 90, common.LogExportMaxDays)
		operation_setting.GetLogSetting().MaxRangeDays, common.LogExportMaxDays = 1, 1
		loadOptionsFromDatabase()
		assert.Equal(t, 90, operation_setting.GetLogSetting().MaxRangeDays)
		assert.Equal(t, 90, common.LogExportMaxDays)
	}
	for _, value := range []string{"0", "-1", "", "1.5", "invalid"} {
		for _, key := range []string{operation_setting.LogMaxRangeDaysOptionKey, legacyLogExportMaxDaysOptionKey} {
			require.Error(t, UpdateOption(key, value))
		}
	}
	assert.Equal(t, "90", requireOptionValue(t, db, operation_setting.LogMaxRangeDaysOptionKey))
	assert.Equal(t, "90", requireOptionValue(t, db, legacyLogExportMaxDaysOptionKey))
}

func TestLogRangeOptionUpdateRollsBackBothAliases(t *testing.T) {
	db := useFrontendOptionMigrationDB(t)
	preserveLogRangeOptionState(t)
	require.NoError(t, UpdateOption(operation_setting.LogMaxRangeDaysOptionKey, "30"))
	writes := 0
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:fail-second-log-range-update", func(tx *gorm.DB) {
		writes++
		if writes == 2 {
			tx.AddError(errors.New("forced second option write failure"))
		}
	}))
	t.Cleanup(func() { db.Callback().Update().Remove("test:fail-second-log-range-update") })
	require.Error(t, UpdateOption(operation_setting.LogMaxRangeDaysOptionKey, "90"))
	for _, key := range []string{operation_setting.LogMaxRangeDaysOptionKey, legacyLogExportMaxDaysOptionKey} {
		assert.Equal(t, "30", requireOptionValue(t, db, key))
		assert.Equal(t, "30", common.OptionMap[key])
	}
	assert.Equal(t, 30, operation_setting.GetLogSetting().MaxRangeDays)
}

func TestLogRangeMigrationFailuresKeepTheStricterRuntimeLimit(t *testing.T) {
	for _, failRead := range []bool{true, false} {
		t.Run(map[bool]string{true: "read failure", false: "upsert failure"}[failRead], func(t *testing.T) {
			db := useFrontendOptionMigrationDB(t)
			preserveLogRangeOptionState(t)
			require.NoError(t, db.Create(&[]Option{
				{Key: operation_setting.LogMaxRangeDaysOptionKey, Value: "90"},
				{Key: legacyLogExportMaxDaysOptionKey, Value: "7"},
			}).Error)
			if failRead {
				reads := 0
				require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:fail-log-range-read", func(tx *gorm.DB) {
					reads++
					if reads == 1 {
						tx.AddError(errors.New("forced migration read failure"))
					}
				}))
				t.Cleanup(func() { db.Callback().Query().Remove("test:fail-log-range-read") })
			} else {
				require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:fail-log-range-upsert", func(tx *gorm.DB) {
					tx.AddError(errors.New("forced migration upsert failure"))
				}))
				t.Cleanup(func() { db.Callback().Create().Remove("test:fail-log-range-upsert") })
			}
			operation_setting.GetLogSetting().MaxRangeDays, common.LogExportMaxDays = 365, 30
			loadOptionsFromDatabase()
			assert.Equal(t, 7, operation_setting.GetLogSetting().MaxRangeDays)
			assert.Equal(t, 7, common.LogExportMaxDays)
			assert.Equal(t, "90", requireOptionValue(t, db, operation_setting.LogMaxRangeDaysOptionKey))
			assert.Equal(t, "7", requireOptionValue(t, db, legacyLogExportMaxDaysOptionKey))
		})
	}
}

func TestLogRangeMigrationReadFailureKeepsKnownRuntimeFallbacks(t *testing.T) {
	for _, key := range []string{operation_setting.LogMaxRangeDaysOptionKey, legacyLogExportMaxDaysOptionKey} {
		t.Run(key, func(t *testing.T) {
			db := useFrontendOptionMigrationDB(t)
			preserveLogRangeOptionState(t)
			require.NoError(t, db.Create(&Option{Key: key, Value: "90"}).Error)
			reads := 0
			require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:fail-partial-range-read", func(tx *gorm.DB) {
				reads++
				if reads == 1 {
					tx.AddError(errors.New("forced migration read failure"))
				}
			}))
			t.Cleanup(func() { db.Callback().Query().Remove("test:fail-partial-range-read") })
			operation_setting.GetLogSetting().MaxRangeDays, common.LogExportMaxDays = 7, 7
			loadOptionsFromDatabase()
			assert.Equal(t, 7, operation_setting.GetLogSetting().MaxRangeDays)
			assert.Equal(t, 7, common.LogExportMaxDays)
		})
	}
}
