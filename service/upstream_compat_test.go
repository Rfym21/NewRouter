package service

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"new-router/common"
	"new-router/constant"
	"new-router/model"
	"new-router/setting"
	"new-router/setting/ratio_setting"
	"new-router/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelExportProofBindsExactSelection(t *testing.T) {
	bind := func(raw string) (VerificationBinding, error) {
		return BindVerificationOperation(VerificationOperation{
			Scope: VerificationScopeChannelKeyExport, Context: json.RawMessage(raw),
		})
	}
	selected, err := bind(`{"channel_ids":[3,1,3]}`)
	require.NoError(t, err)
	reordered, err := bind(`{"channel_ids":[1,3]}`)
	require.NoError(t, err)
	assert.Equal(t, selected, reordered)
	other, err := bind(`{"channel_ids":[1,4]}`)
	require.NoError(t, err)
	assert.NotEqual(t, selected.ContextHash, other.ContextHash)
	all, err := bind(`{"channel_ids":[]}`)
	require.NoError(t, err)
	assert.NotEqual(t, selected.ContextHash, all.ContextHash)
	for _, raw := range []string{`{}`, `{"channel_ids":null}`, `{"channel_ids":[0]}`, `{"channel_ids":[-1]}`, `{"channel_ids":[1],"other":true}`} {
		_, err := bind(raw)
		assert.ErrorIs(t, err, ErrVerificationContextInvalid, raw)
	}
	methods, err := securityVerificationPolicy(VerificationScopeChannelKeyExport, model.UserVerificationState{HasPassword: true})
	require.NoError(t, err)
	assert.Empty(t, methods, "export must not fall back from MFA to password-only verification")
	methods, err = securityVerificationPolicy(VerificationScopeChannelKeyExport, model.UserVerificationState{HasTwoFA: true})
	require.NoError(t, err)
	require.Len(t, methods, 1)
	assert.Equal(t, VerificationMethodTwoFA, methods[0].Method)
}

func TestTokenAutoGroupsPreservePerUserOverrides(t *testing.T) {
	originalGroups := setting.UserUsableGroups2JSONString()
	originalAuto := setting.AutoGroups2JsonString()
	originalRatios := ratio_setting.GroupRatio2JSONString()
	groupSettings := ratio_setting.GetGroupRatioSetting()
	originalSpecial := groupSettings.GroupSpecialUsableGroup
	originalUserSpecial := groupSettings.UserSpecialUsableGroup
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalGroups))
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(originalAuto))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalRatios))
		groupSettings.GroupSpecialUsableGroup = originalSpecial
		groupSettings.UserSpecialUsableGroup = originalUserSpecial
	})
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","blocked":"Blocked"}`))
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["default","blocked","personal","personal"]`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"blocked":1,"personal":0.5}`))
	groupSettings.GroupSpecialUsableGroup = types.NewRWMap[string, map[string]string]()
	groupSettings.UserSpecialUsableGroup = types.NewRWMap[string, map[string]string]()
	groupSettings.UserSpecialUsableGroup.Set("7", map[string]string{"-:blocked": "", "+:personal": "Personal"})
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(c, constant.ContextKeyUserId, 7)
	assert.Equal(t, []string{"default", "personal"}, GetRequestAutoGroups(c, "default"))
	common.SetContextKey(c, constant.ContextKeyTokenAutoGroups, []string{"blocked", "personal", "personal", "default"})
	assert.Equal(t, []string{"personal", "default"}, GetRequestAutoGroups(c, "default"))
	common.SetContextKey(c, constant.ContextKeyTokenAutoGroups, []string{})
	assert.Empty(t, GetRequestAutoGroups(c, "default"), "an explicit empty token selection must not regain inherited groups")
	assert.False(t, GroupInUserUsableGroups("default", "personal", 8))
	assert.True(t, GroupInUserUsableGroups("default", "personal", 7))
}

func TestFailedTaskReconciliationRefundsAllAccountingOnce(t *testing.T) {
	truncate(t)
	seedUser(t, 1, 1000)
	seedToken(t, 1, 1, "reconciliation-test-key", 2000)
	seedChannel(t, 1)
	seedChargedAccounting(t, 1, 1, 1, 300, 1)
	task := makeTask(1, 1, 300, 1, BillingSourceWallet, 0)
	task.TaskID = "pending-refund-before-upstream-cutoff"
	task.Status = model.TaskStatusFailure
	task.Progress = "100%"
	task.SubmitTime = time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC).Unix()
	task.UpdatedAt = time.Now().Add(-2 * refundReconciliationGracePeriod).Unix()
	require.NoError(t, model.DB.Create(task).Error)
	sweepUnrefundedFailedTasks(context.Background())
	sweepUnrefundedFailedTasks(context.Background())
	assert.Equal(t, 1300, getUserQuota(t, 1))
	assert.Equal(t, 2300, getTokenRemainQuota(t, 1))
	assert.Zero(t, getTokenUsedQuota(t, 1))
	used, requests := getUserUsageAccounting(t, 1)
	assert.Zero(t, used)
	assert.Equal(t, 1, requests)
	assert.Zero(t, getChannelUsedQuota(t, 1))
	assert.Zero(t, getTaskQuota(t, task.ID))
	var refunds int64
	require.NoError(t, model.DB.Model(&model.Log{}).Where("type = ?", model.LogTypeRefund).Count(&refunds).Error)
	assert.EqualValues(t, 1, refunds)
}
