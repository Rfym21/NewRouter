package model

import (
	"maps"
	"strconv"
	"sync"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"new-router/setting/operation_setting"
)

// Keep the former export option as a compatibility alias. Both names now set
// the same limit, and must be persisted together so a reload cannot undo it.
const legacyLogExportMaxDaysOptionKey = "LogExportMaxDays"

var logRangeOptionMutex sync.Mutex

func isLogRangeOption(key string) bool {
	return key == operation_setting.LogMaxRangeDaysOptionKey || key == legacyLogExportMaxDaysOptionKey
}

func normalizedLogRangeDays(value string) int {
	days, err := strconv.Atoi(value)
	if err != nil || days < 1 {
		return 1
	}
	return days
}

func mergedLogRangeOptions(options []*Option, queryDefault, exportDefault int) []*Option {
	values := map[string]string{
		operation_setting.LogMaxRangeDaysOptionKey: strconv.Itoa(max(queryDefault, 1)),
		legacyLogExportMaxDaysOptionKey:            strconv.Itoa(max(exportDefault, 1)),
	}
	hasRange := false
	for _, option := range options {
		if isLogRangeOption(option.Key) {
			values[option.Key] = option.Value
			hasRange = true
		}
	}
	if !hasRange {
		return nil
	}
	days := strconv.Itoa(min(
		normalizedLogRangeDays(values[operation_setting.LogMaxRangeDaysOptionKey]),
		normalizedLogRangeDays(values[legacyLogExportMaxDaysOptionKey]),
	))
	return []*Option{
		{Key: operation_setting.LogMaxRangeDaysOptionKey, Value: days},
		{Key: legacyLogExportMaxDaysOptionKey, Value: days},
	}
}

func logRangeOptionUpdates(values map[string]string) (map[string]string, error) {
	canonical, hasCanonical := values[operation_setting.LogMaxRangeDaysOptionKey]
	legacy, hasLegacy := values[legacyLogExportMaxDaysOptionKey]
	if !hasCanonical && !hasLegacy {
		return values, nil
	}
	for _, value := range []struct {
		raw     string
		present bool
	}{{canonical, hasCanonical}, {legacy, hasLegacy}} {
		if value.present {
			if err := operation_setting.ValidateLogMaxRangeDays(value.raw); err != nil {
				return nil, err
			}
		}
	}
	if !hasCanonical {
		canonical = legacy
	} else if hasLegacy {
		canonical = strconv.Itoa(min(normalizedLogRangeDays(canonical), normalizedLogRangeDays(legacy)))
	}
	updates := maps.Clone(values)
	updates[operation_setting.LogMaxRangeDaysOptionKey] = canonical
	updates[legacyLogExportMaxDaysOptionKey] = canonical
	return updates, nil
}

// Old installations allowed queries for 365 days and background exports for
// 30 days by default. Preserve the stricter effective limit when merging saved
// settings, including installations that saved only one of the former keys.
func migrateLogRangeOptions(options []*Option) ([]*Option, error) {
	logRangeOptionMutex.Lock()
	defer logRangeOptionMutex.Unlock()
	result := options
	err := DB.Transaction(func(tx *gorm.DB) error {
		var saved []Option
		keys := []string{operation_setting.LogMaxRangeDaysOptionKey, legacyLogExportMaxDaysOptionKey}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(map[string]any{"key": keys}).Find(&saved).Error; err != nil {
			return err
		}
		if len(saved) == 0 {
			return nil
		}
		values := map[string]string{
			operation_setting.LogMaxRangeDaysOptionKey: "365",
			legacyLogExportMaxDaysOptionKey:            "30",
		}
		for _, option := range saved {
			values[option.Key] = option.Value
		}
		days := strconv.Itoa(min(
			normalizedLogRangeDays(values[operation_setting.LogMaxRangeDaysOptionKey]),
			normalizedLogRangeDays(values[legacyLogExportMaxDaysOptionKey]),
		))
		result = make([]*Option, 0, len(options)+2)
		for _, option := range options {
			if !isLogRangeOption(option.Key) {
				result = append(result, option)
			}
		}
		for _, key := range keys {
			result = append(result, &Option{Key: key, Value: days})
		}
		if len(saved) == 2 && values[keys[0]] == days && values[keys[1]] == days {
			return nil
		}
		updates := []Option{{Key: keys[0], Value: days}, {Key: keys[1], Value: days}}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "key"}},
			DoUpdates: clause.AssignmentColumns([]string{"value"}),
		}).Create(&updates).Error
	})
	return result, err
}
