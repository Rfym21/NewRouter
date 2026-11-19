package model

import (
	"testing"

	"new-router/common"
	"new-router/pkg/clientua"
	"new-router/setting/operation_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func consumeLogWithUserAgent(t *testing.T, userAgent string) (*Log, map[string]any) {
	t.Helper()
	database, ctx, _ := setupAnonymizationLogTest(t)
	ctx.Request.Header.Set("User-Agent", userAgent)
	RecordConsumeLog(ctx, anonymizationLogTestUserID, RecordConsumeLogParams{
		ChannelId: 7,
		ModelName: "claude-sonnet-4-5",
		TokenName: "test-token",
		Quota:     12,
		Group:     "default",
		Other:     NewLogOther(),
	})

	var stored Log
	require.NoError(t, database.First(&stored).Error)
	parsed, err := common.StrToMap(stored.Other)
	require.NoError(t, err)
	return &stored, parsed
}

func TestConsumeLogStoresRequestClient(t *testing.T) {
	stored, parsed := consumeLogWithUserAgent(t, "claude-cli/2.1.197 (external, cli)")

	assert.Equal(t, "Claude Code", stored.Client)
	assert.Equal(t, "ai_agent", parsed["client_category"])
	assert.Equal(t, "lobe:ClaudeCode", parsed["client_icon"])
	admin, ok := parsed["admin_info"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "claude-cli/2.1.197 (external, cli)", admin["user_agent"])
}

func TestConsumeLogCustomClientRuleWins(t *testing.T) {
	previous := operation_setting.GetLogSetting().ClientRules
	operation_setting.GetLogSetting().ClientRules = []clientua.CustomRule{
		{Name: "Private Claude Wrapper", Keywords: []string{"claude-cli"}},
	}
	t.Cleanup(func() {
		operation_setting.GetLogSetting().ClientRules = previous
	})

	stored, parsed := consumeLogWithUserAgent(t, "claude-cli/2.1.197 (external, cli)")

	assert.Equal(t, "Private Claude Wrapper", stored.Client)
	assert.Equal(t, "custom", parsed["client_category"])
	assert.Equal(t, "lu:Terminal", parsed["client_icon"])
}

func TestConsumeLogUnknownAndEmptyUserAgent(t *testing.T) {
	stored, parsed := consumeLogWithUserAgent(t, "custom-client/1.0")
	assert.Equal(t, "Other", stored.Client)
	assert.Equal(t, "other", parsed["client_category"])
	assert.Equal(t, "lu:CircleHelp", parsed["client_icon"])

	stored, parsed = consumeLogWithUserAgent(t, "")
	assert.Empty(t, stored.Client)
	_, hasCategory := parsed["client_category"]
	assert.False(t, hasCategory)
}

func TestConsumeLogTruncatesAdminUserAgent(t *testing.T) {
	long := make([]byte, 700)
	for i := range long {
		long[i] = 'a'
	}
	stored, parsed := consumeLogWithUserAgent(t, string(long))
	admin, ok := parsed["admin_info"].(map[string]any)
	require.True(t, ok)
	assert.Len(t, []rune(admin["user_agent"].(string)), 512)
	assert.Equal(t, "Other", stored.Client)
}

func TestRecordErrorLogStoresRequestClient(t *testing.T) {
	database, ctx, _ := setupAnonymizationLogTest(t)
	ctx.Request.Header.Set("User-Agent", "Go-http-client/1.1")
	other := NewLogOther()
	RecordErrorLog(ctx, anonymizationLogTestUserID, 7, "gpt-test", "test-token", "failed", 1, 1, false, "default", other)

	var stored Log
	require.NoError(t, database.First(&stored).Error)
	assert.Equal(t, "Go net/http", stored.Client)
	parsed, err := common.StrToMap(stored.Other)
	require.NoError(t, err)
	assert.Equal(t, "http_client", parsed["client_category"])
}
