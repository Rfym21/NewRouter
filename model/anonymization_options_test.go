package model

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"new-router/common"
	"new-router/pkg/anonymizer"
	"new-router/setting"
)

func TestCustomAnonymizationRulesPersistAndRefreshAtomically(t *testing.T) {
	db := setupAuditToggleTest(t)
	InitOptionMap()
	key := setting.AnonymizationRulesOptionKey
	assert.Equal(t, "[]", CurrentRequestPolicy().Options[key])
	rules := `[{"name":"private project","mode":"literal","pattern":"ProjectSecret","enabled":true}]`
	require.NoError(t, UpdateOption(key, rules))
	assert.Equal(t, rules, requireOptionValue(t, db, key))
	policy := CurrentRequestPolicy()
	masked := anonymizer.NewSession("test", nil).WithRules(policy.AnonymizationRules).AnonymizeString("ProjectSecret")
	assert.NotEqual(t, "ProjectSecret", masked)
	loadOptionsFromDatabase()
	assert.Equal(t, rules, CurrentRequestPolicy().Options[key])
	assert.NotEmpty(t, CurrentRequestPolicy().AnonymizationRules.Detect("ProjectSecret"))
	for _, invalid := range []string{`null`, `{}`, `[] true`, `[{"name":"bad","mode":"regex","pattern":"(?<=private)secret","enabled":true}]`, `[{"name":"bad","mode":"literal","pattern":"secret","enabled":true,"unknown":1}]`} {
		before := CurrentRequestPolicy()
		assert.Error(t, UpdateOption(key, invalid))
		assert.Same(t, before, CurrentRequestPolicy())
		assert.Equal(t, rules, requireOptionValue(t, db, key))
		assert.Equal(t, rules, common.OptionMap[key])
	}
	const callback = "anonymization_rules_failed_save"
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) { tx.AddError(errors.New("test database failure")) }))
	before := CurrentRequestPolicy()
	assert.Error(t, UpdateOption(key, "[]"))
	assert.Same(t, before, CurrentRequestPolicy())
	assert.Equal(t, rules, common.OptionMap[key])
	assert.Equal(t, rules, requireOptionValue(t, db, key))
	require.NoError(t, db.Callback().Update().Remove(callback))
	require.NoError(t, UpdateRequestPolicyOptions(map[string]string{key: "[]"}))
	assert.Empty(t, CurrentRequestPolicy().AnonymizationRules.Detect("ProjectSecret"))
	assert.NotEmpty(t, policy.AnonymizationRules.Detect("ProjectSecret"), "in-flight requests keep their immutable rule snapshot")
}

func TestStrictAnonymizationContextDefaultsPersistenceAndHotReload(t *testing.T) {
	db := setupAuditToggleTest(t)
	previous := setting.AnonymizationStrictContextEnabled
	t.Cleanup(func() { setting.AnonymizationStrictContextEnabled = previous })
	setting.AnonymizationStrictContextEnabled = false
	InitOptionMap()
	const key = "AnonymizationStrictContextEnabled"
	assert.Equal(t, "false", CurrentRequestPolicy().Options[key])
	for _, value := range []string{"true", "false"} {
		require.NoError(t, UpdateOption(key, value))
		assert.Equal(t, value, requireOptionValue(t, db, key))
		assert.Equal(t, value, CurrentRequestPolicy().Options[key])
		assert.Equal(t, value == "true", setting.AnonymizationStrictContextEnabled)
		setting.AnonymizationStrictContextEnabled = value != "true"
		InitOptionMap()
		assert.Equal(t, value, CurrentRequestPolicy().Options[key])
		assert.Equal(t, value == "true", setting.AnonymizationStrictContextEnabled)
	}
	require.NoError(t, db.Model(&Option{}).Where("key = ?", key).Update("value", "true").Error)
	loadOptionsFromDatabase()
	assert.Equal(t, "true", CurrentRequestPolicy().Options[key])
	assert.True(t, setting.AnonymizationStrictContextEnabled)
	before := CurrentRequestPolicy()
	assert.Error(t, UpdateOption(key, "invalid"))
	assert.Same(t, before, CurrentRequestPolicy())
	assert.Equal(t, "true", requireOptionValue(t, db, key))
}
