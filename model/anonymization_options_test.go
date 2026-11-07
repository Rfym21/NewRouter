package model

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"new-router/common"
	"new-router/pkg/anonymizer"
	"new-router/setting"
	"new-router/setting/config"
	"new-router/setting/runtime_setting"
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

func TestAnonymizationAnalyzerPersistenceValidationAndHotReload(t *testing.T) {
	db := setupAuditToggleTest(t)
	previousDir, previousURL := runtime_setting.GetAnonymizationAnalyzerConfig()
	setRuntimeConfig := func(modelDir, endpoint string) {
		t.Helper()
		require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("runtime_setting"), map[string]string{
			"anonymization_local_ner_model_dir": modelDir,
			"anonymization_presidio_url":        endpoint,
		}))
		runtime_setting.ApplyToRuntime()
	}
	t.Cleanup(func() { setRuntimeConfig(previousDir, previousURL) })
	setRuntimeConfig("", "")
	InitOptionMap()

	analyzer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/analyze", r.URL.Path)
		_, err := fmt.Fprint(w, `[{"entity_type":"PERSON","start":0,"end":4,"score":0.95}]`)
		assert.NoError(t, err)
	}))
	defer analyzer.Close()
	require.NoError(t, UpdateOption(anonymizationPresidioURLOption, analyzer.URL))
	assert.Equal(t, analyzer.URL, requireOptionValue(t, db, anonymizationPresidioURLOption))
	detector, err := setting.AnonymizationDetector()
	require.NoError(t, err)
	require.NotNil(t, detector)
	selectedDetector := detector
	matches, err := detector.Detect(context.Background(), "Jane")
	require.NoError(t, err)
	assert.Len(t, matches, 1, "saved runtime settings must reach the actual detector")

	for _, invalid := range []string{"invalid", "ftp://analyzer", "https://user@analyzer", "https://analyzer?key=secret", "https://analyzer#fragment"} {
		assert.Error(t, UpdateOption(anonymizationPresidioURLOption, invalid))
		assert.Equal(t, analyzer.URL, requireOptionValue(t, db, anonymizationPresidioURLOption))
		_, endpoint := runtime_setting.GetAnonymizationAnalyzerConfig()
		assert.Equal(t, analyzer.URL, endpoint)
	}
	// Simulate an instance whose cache has not observed another instance's save.
	common.OptionMapRWMutex.Lock()
	common.OptionMap[anonymizationModelDirOption] = ""
	common.OptionMap[anonymizationPresidioURLOption] = ""
	common.OptionMapRWMutex.Unlock()
	assert.Error(t, UpdateOption(anonymizationModelDirOption, "/models/guard"))
	assert.Empty(t, requireOptionValue(t, db, anonymizationModelDirOption))
	assert.Equal(t, analyzer.URL, requireOptionValue(t, db, anonymizationPresidioURLOption))
	loadOptionsFromDatabase()

	require.NoError(t, UpdateOptionsBulk(map[string]string{
		anonymizationModelDirOption:    "/models/guard",
		anonymizationPresidioURLOption: "",
	}))
	modelDir, endpoint := runtime_setting.GetAnonymizationAnalyzerConfig()
	assert.Equal(t, "/models/guard", modelDir)
	assert.Empty(t, endpoint)
	assert.Equal(t, "/models/guard", requireOptionValue(t, db, anonymizationModelDirOption))
	setRuntimeConfig("", analyzer.URL)
	InitOptionMap()
	modelDir, endpoint = runtime_setting.GetAnonymizationAnalyzerConfig()
	assert.Equal(t, "/models/guard", modelDir)
	assert.Empty(t, endpoint, "reload must apply the saved pair together")

	const callback = "anonymization_analyzer_failed_save"
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		tx.AddError(errors.New("test database failure"))
	}))
	err = UpdateOptionsBulk(map[string]string{
		anonymizationModelDirOption:    "",
		anonymizationPresidioURLOption: analyzer.URL,
	})
	require.NoError(t, db.Callback().Update().Remove(callback))
	require.Error(t, err)
	modelDir, endpoint = runtime_setting.GetAnonymizationAnalyzerConfig()
	assert.Equal(t, "/models/guard", modelDir)
	assert.Empty(t, endpoint)
	assert.Equal(t, "/models/guard", requireOptionValue(t, db, anonymizationModelDirOption))
	assert.Empty(t, requireOptionValue(t, db, anonymizationPresidioURLOption))

	require.NoError(t, UpdateOptionsBulk(map[string]string{
		anonymizationModelDirOption:    "",
		anonymizationPresidioURLOption: "",
	}))
	t.Setenv("ANONYMIZATION_LOCAL_NER_MODEL_DIR", "/models/environment-default")
	t.Setenv("ANONYMIZATION_PRESIDIO_URL", analyzer.URL)
	runtime_setting.InitAnonymizationAnalyzerDefaults()
	runtime_setting.ApplyToRuntime()
	modelDir, endpoint = runtime_setting.GetAnonymizationAnalyzerConfig()
	assert.Equal(t, "/models/environment-default", modelDir)
	assert.Equal(t, analyzer.URL, endpoint)
	InitOptionMap()
	modelDir, endpoint = runtime_setting.GetAnonymizationAnalyzerConfig()
	assert.Empty(t, modelDir, "saved empty strings must override environment defaults")
	assert.Empty(t, endpoint)
	detector, err = setting.AnonymizationDetector()
	require.NoError(t, err)
	assert.Nil(t, detector)
	// A detector already selected by an in-flight request keeps its configuration.
	matches, err = selectedDetector.Detect(context.Background(), "Jane")
	require.NoError(t, err)
	assert.Len(t, matches, 1)

	require.NoError(t, UpdateOptionsBulk(map[string]string{
		"passkey.rp_id":                "",
		anonymizationModelDirOption:    "",
		anonymizationPresidioURLOption: analyzer.URL,
	}))
	modelDir, endpoint = runtime_setting.GetAnonymizationAnalyzerConfig()
	assert.Empty(t, modelDir)
	assert.Equal(t, analyzer.URL, endpoint, "mixed bulk updates must publish the analyzer pair together")
	_, err = UpdatePasskeyDomainOptions(map[string]string{
		"passkey.rp_id":             "",
		anonymizationModelDirOption: "/models/guard",
	}, false, "")
	assert.Error(t, err)
	assert.Empty(t, requireOptionValue(t, db, anonymizationModelDirOption))
	assert.Equal(t, analyzer.URL, requireOptionValue(t, db, anonymizationPresidioURLOption))
}
