package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"new-router/common"
	"new-router/pkg/anonymizer"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const anonymizationLogTestUserID = 87601
const anonymizationLogTestOriginal = "PRIVATE_CASE_4872"

// These tests use global database and log-writer handles and must stay serial.
func setupAnonymizationLogTest(t *testing.T) (*gorm.DB, *gin.Context, *bytes.Buffer) {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	require.NoError(t, err)
	sqlDB, err := database.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, database.AutoMigrate(&User{}, &Log{}))
	require.NoError(t, database.Create(&User{Id: anonymizationLogTestUserID, Username: "anonymization-owner", Group: "default", Setting: "{}"}).Error)

	previousDB, previousLogDB := DB, LOG_DB
	previousRedis, previousConsume, previousExport := common.RedisEnabled, common.LogConsumeEnabled, common.DataExportEnabled
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	DB, LOG_DB = database, database
	common.RedisEnabled, common.LogConsumeEnabled, common.DataExportEnabled = false, true, false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)

	output := &bytes.Buffer{}
	common.LogWriterMu.Lock()
	previousWriter, previousErrorWriter := gin.DefaultWriter, gin.DefaultErrorWriter
	gin.DefaultWriter, gin.DefaultErrorWriter = output, output
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultWriter, gin.DefaultErrorWriter = previousWriter, previousErrorWriter
		common.LogWriterMu.Unlock()
		DB, LOG_DB = previousDB, previousLogDB
		common.RedisEnabled, common.LogConsumeEnabled, common.DataExportEnabled = previousRedis, previousConsume, previousExport
		common.SetDatabaseTypes(previousMainType, previousLogType)
		assert.NoError(t, sqlDB.Close())
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Set("username", "anonymization-owner")
	ctx.Set(common.RequestIdKey, "anonymization-request")
	ctx.Set(common.UpstreamRequestIdKey, "anonymization-upstream")
	return database, ctx, output
}

func anonymizationLogTestSession(t *testing.T, text string) *anonymizer.Session {
	t.Helper()
	rules, err := anonymizer.CompileRules([]anonymizer.Rule{{Name: "private-case", Mode: "literal", Pattern: anonymizationLogTestOriginal, EntityType: "CUSTOM", Enabled: true}})
	require.NoError(t, err)
	session := anonymizer.NewSession("log-test", nil).WithRules(rules)
	raw, err := common.Marshal(map[string]any{"messages": []any{map[string]any{"role": "user", "content": text}}})
	require.NoError(t, err)
	_, err = anonymizer.AnonymizeChatRequest(raw, session)
	require.NoError(t, err)
	return session
}

func requireAnonymizationLogCount(t *testing.T, otherJSON string, count int64) map[string]any {
	t.Helper()
	var raw map[string]json.RawMessage
	require.NoError(t, common.UnmarshalJsonStr(otherJSON, &raw))
	// Check the stored JSON token, not a float conversion or a quoted string.
	require.Equal(t, fmt.Sprint(count), string(raw["anonymization_count"]))
	var other map[string]any
	require.NoError(t, common.UnmarshalWithNumber([]byte(otherJSON), &other))
	return other
}

func TestAnonymizationConsumeLogStoresOnlyCountAndRedactsDiagnostics(t *testing.T) {
	database, ctx, output := setupAnonymizationLogTest(t)
	session := anonymizationLogTestSession(t, anonymizationLogTestOriginal+" / "+anonymizationLogTestOriginal)
	require.EqualValues(t, 2, session.ReplacementCount())
	anonymizer.Attach(ctx, session)
	alias := session.Snapshot()[0].Surrogate

	type policyDiagnostic struct {
		Reason  string `json:"reason"`
		Attempt int    `json:"attempt"`
	}
	other := NewLogOther()
	other.SetPublic("model_price", 0.125)
	other.SetPublic("cache_tokens", 12)
	other.SetPublic("billing_source", "subscription")
	other.SetPublic("po", []string{"set instructions = " + anonymizationLogTestOriginal, "append instructions with " + alias})
	other.SetPublic("stream_status", map[string]any{"status": "error", "end_reason": "upstream_eof", "errors": []string{anonymizationLogTestOriginal, alias}, "error_count": 2})
	other.SetAdmin("reject_reason", "upstream rejected "+anonymizationLogTestOriginal)
	other.SetAdmin("request_policy", []policyDiagnostic{{Reason: alias, Attempt: 1}})
	other.SetRoot("generation", 7)
	message := common.NewMessage("Processed {{value}} ({{tokens}} tokens)", map[string]any{"value": anonymizationLogTestOriginal, "tokens": 12})

	RecordConsumeLog(ctx, anonymizationLogTestUserID, RecordConsumeLogParams{
		ChannelId: 9, PromptTokens: 12, CompletionTokens: 4, ModelName: "gpt-test", TokenName: "safe-token", Quota: 125,
		Content: []*common.Message{message}, TokenId: 17, UseTimeSeconds: 2, IsStream: true, Group: "default", Other: other,
	})
	var stored Log
	require.NoError(t, database.First(&stored).Error)
	parsed := requireAnonymizationLogCount(t, stored.Other, 2)
	assert.Equal(t, json.Number("0.125"), parsed["model_price"])
	assert.Equal(t, json.Number("12"), parsed["cache_tokens"])
	assert.Equal(t, "subscription", parsed["billing_source"])
	assert.Equal(t, []any{"set instructions = [REDACTED]", "append instructions with [REDACTED]"}, parsed["po"])
	stream := parsed["stream_status"].(map[string]any)
	assert.Equal(t, "upstream_eof", stream["end_reason"])
	assert.Equal(t, json.Number("2"), stream["error_count"])
	assert.Equal(t, []any{"[REDACTED]", "[REDACTED]"}, stream["errors"])
	admin := parsed["admin_info"].(map[string]any)
	assert.Equal(t, "upstream rejected [REDACTED]", admin["reject_reason"])
	assert.Equal(t, "[REDACTED]", admin["request_policy"].([]any)[0].(map[string]any)["reason"])
	assert.Equal(t, "Processed [REDACTED] (12 tokens)", stored.Content)
	assert.Equal(t, anonymizationLogTestOriginal, message.Params["value"], "logging must not mutate request-owned message values")
	assert.Equal(t, 125, stored.Quota)
	assert.Equal(t, 12, stored.PromptTokens)
	assert.Equal(t, 4, stored.CompletionTokens)
	assert.Equal(t, "anonymization-request", stored.RequestId)
	for _, value := range []string{anonymizationLogTestOriginal, alias} {
		assert.NotContains(t, stored.Other, value)
		assert.NotContains(t, stored.Content, value)
		assert.NotContains(t, output.String(), value, "INFO serialization must use the sanitized metadata and message")
	}
	assert.Contains(t, output.String(), `"anonymization_count":2`)
	assert.EqualValues(t, 2, session.ReplacementCount(), "redaction must not affect the request count")

	for _, visibility := range []string{"owner", "admin", "root"} {
		t.Run(visibility, func(t *testing.T) {
			copyLog := stored
			logs := []*Log{&copyLog}
			switch visibility {
			case "owner":
				formatUserLogs(logs, 0)
			case "admin":
				FormatAdminLogs(logs)
			case "root":
				FormatRootLogs(logs)
			}
			projected := requireAnonymizationLogCount(t, copyLog.Other, 2)
			if visibility == "owner" {
				assert.NotContains(t, projected, "admin_info")
			}
			if visibility != "root" {
				assert.NotContains(t, projected, "root_info")
			}
		})
	}
	assert.Contains(t, NormalizeLogExportColumns([]string{"anonymization_count"}, false), "anonymization_count")
	assert.Equal(t, []string{"2", "订阅抵扣: 125"}, buildLogExportRow(&stored, []string{"anonymization_count", "cost"}))
}

func TestAnonymizationErrorLogWithNilOtherRedactsStoredAndServerText(t *testing.T) {
	database, ctx, output := setupAnonymizationLogTest(t)
	session := anonymizationLogTestSession(t, anonymizationLogTestOriginal)
	anonymizer.Attach(ctx, session)
	alias := session.Snapshot()[0].Surrogate
	RecordErrorLog(ctx, anonymizationLogTestUserID, 9, "gpt-test", "safe-token", "upstream rejected "+anonymizationLogTestOriginal+" / "+alias, 17, 2, true, "default", nil)
	var stored Log
	require.NoError(t, database.First(&stored).Error)
	other := requireAnonymizationLogCount(t, stored.Other, 1)
	assert.Len(t, other, 1, "the new metadata contains only a count")
	assert.Equal(t, "upstream rejected [REDACTED] / [REDACTED]", stored.Content)
	assert.Equal(t, LogTypeError, stored.Type)
	for _, value := range []string{anonymizationLogTestOriginal, alias} {
		assert.NotContains(t, stored.Other, value)
		assert.NotContains(t, stored.Content, value)
		assert.NotContains(t, output.String(), value)
	}
}

func TestAnonymizationConsumeLogWithNilOtherKeepsCount(t *testing.T) {
	database, ctx, _ := setupAnonymizationLogTest(t)
	anonymizer.Attach(ctx, anonymizationLogTestSession(t, anonymizationLogTestOriginal))
	RecordConsumeLog(ctx, anonymizationLogTestUserID, RecordConsumeLogParams{ModelName: "gpt-test", Group: "default"})
	var stored Log
	require.NoError(t, database.First(&stored).Error)
	assert.Len(t, requireAnonymizationLogCount(t, stored.Other, 1), 1)
}

func TestAnonymizationLogsOmitCountWhenNoRequestValuesWereProcessed(t *testing.T) {
	for _, scenario := range []string{"no session", "no matches", "imported history only", "disabled retry"} {
		t.Run(scenario, func(t *testing.T) {
			database, ctx, output := setupAnonymizationLogTest(t)
			switch scenario {
			case "no matches":
				anonymizer.Attach(ctx, anonymizationLogTestSession(t, "hello world"))
			case "imported history only":
				session := anonymizationLogTestSession(t, "hello world")
				require.NoError(t, session.Import([]anonymizer.Mapping{{EntityType: "CUSTOM", Original: anonymizationLogTestOriginal, Surrogate: "[CUSTOM_HISTORY_18742]"}}))
				anonymizer.Attach(ctx, session)
			case "disabled retry":
				anonymizer.Attach(ctx, anonymizationLogTestSession(t, anonymizationLogTestOriginal))
				anonymizer.ClearContextForDisabledChannel(ctx)
			}
			RecordConsumeLog(ctx, anonymizationLogTestUserID, RecordConsumeLogParams{ModelName: "gpt-test", Group: "default"})
			RecordErrorLog(ctx, anonymizationLogTestUserID, 9, "gpt-test", "safe-token", "upstream failed", 17, 1, false, "default", nil)
			var logs []Log
			require.NoError(t, database.Order("id").Find(&logs).Error)
			require.Len(t, logs, 2)
			for _, log := range logs {
				assert.NotContains(t, log.Other, "anonymization_count")
				assert.Equal(t, "", getLogExportValue(&log, "anonymization_count"))
			}
			assert.NotContains(t, output.String(), "anonymization_count")
		})
	}
}

func TestAnonymizationLogRedactsJSONEncodedOverrideValues(t *testing.T) {
	database, ctx, output := setupAnonymizationLogTest(t)
	const original = "私密记录_64917\n\"内部编号\""
	rules, err := anonymizer.CompileRules([]anonymizer.Rule{{Name: "private-record", Mode: "literal", Pattern: original, EntityType: "CUSTOM", Enabled: true}})
	require.NoError(t, err)
	session := anonymizer.NewSession("encoded-log-test", nil).WithRules(rules)
	raw, err := common.Marshal(map[string]any{"messages": []any{map[string]any{"role": "user", "content": original}}})
	require.NoError(t, err)
	_, err = anonymizer.AnonymizeChatRequest(raw, session)
	require.NoError(t, err)
	anonymizer.Attach(ctx, session)
	other := NewLogOther()
	other.SetPublic("po", []string{
		"set instructions = " + common.GetJsonString(map[string]any{"note": original}),
		"set messages.0.tool_calls = " + common.GetJsonString([]any{map[string]any{
			"id":   "call_1",
			"type": "function",
			"function": map[string]any{
				"name":      "lookup",
				"arguments": common.GetJsonString(map[string]any{"note": original}),
			},
		}}),
	})
	RecordConsumeLog(ctx, anonymizationLogTestUserID, RecordConsumeLogParams{ModelName: "gpt-test", Group: "default", Other: other})
	var stored Log
	require.NoError(t, database.First(&stored).Error)
	parsed := requireAnonymizationLogCount(t, stored.Other, 1)
	assert.Equal(t, []any{
		"set instructions = [REDACTED]",
		"set messages.0.tool_calls = [REDACTED]",
	}, parsed["po"])
	assert.NotContains(t, stored.Other, "私密记录_64917")
	assert.NotContains(t, output.String(), "私密记录_64917")
}
