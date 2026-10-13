package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"new-router/common"
	"new-router/constant"
	"new-router/model"
	"new-router/pkg/billingexpr"
	relaycommon "new-router/relay/common"
	relayconstant "new-router/relay/constant"
	"new-router/relaykit/dto"
	"new-router/relaykit/types"
	"new-router/setting/operation_setting"
	"new-router/setting/ratio_setting"
	hosttypes "new-router/types"
)

func TestFixedBillingPreservesLocalWaiverAndScheduledUserRatio(t *testing.T) {
	const expression = `tier("request", fixed(0.01)) * (hour("UTC") >= 0 ? 2 : 1)`
	const group = "fixed-compat-group"
	const startingQuota = 1_000_000
	originalBase := ratio_setting.GroupRatio2JSONString()
	originalGroup := ratio_setting.GroupGroupRatio2JSONString()
	originalUser := ratio_setting.UserGroupRatio2JSONString()
	originalWaiver := operation_setting.GetQuotaSetting().EmptyResponseNoBilling
	originalErrors := constant.ErrorLogEnabled
	originalQuotaUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500_000
	constant.ErrorLogEnabled = true
	operation_setting.SetToolPriceForTest("fixed_compat_tool", 4)
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalBase))
		require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(originalGroup))
		require.NoError(t, ratio_setting.UpdateUserGroupRatioByJSONString(originalUser))
		ratio_setting.RefreshScheduleCache(nil)
		operation_setting.GetQuotaSetting().EmptyResponseNoBilling = originalWaiver
		constant.ErrorLogEnabled = originalErrors
		common.QuotaPerUnit = originalQuotaUnit
		operation_setting.DeleteToolPriceForTest("fixed_compat_tool")
	})
	for _, tc := range []struct {
		name       string
		global     bool
		channel    *bool
		completion int
		waived     bool
	}{
		{name: "global empty waiver takes precedence", global: true, waived: true},
		{name: "channel opts back into fixed billing", global: true, channel: common.GetPointer(false)},
		{name: "channel enables empty waiver", channel: common.GetPointer(true), waived: true},
		{name: "nonempty fixed reply keeps schedule and user discount", global: true, completion: 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			truncate(t)
			seedUser(t, 7, startingQuota)
			seedToken(t, 7, 7, "fixed-compat-key", startingQuota)
			seedChannel(t, 7)
			require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"fixed-compat-group":2}`))
			require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(`{"default":{"fixed-compat-group":{"mode":"multiply","value":0.8}}}`))
			require.NoError(t, ratio_setting.UpdateUserGroupRatioByJSONString(`{"7":{"fixed-compat-group":{"mode":"multiply","value":0.5}}}`))
			ratio_setting.RefreshScheduleCache([]ratio_setting.ScheduleWindow{{Id: 1, GroupName: group, StartMinute: 0, EndMinute: 1440, Mode: ratio_setting.RatioModeMultiply, TargetValue: 0.5, StackWithUserRatio: true, Enabled: true}})
			operation_setting.GetQuotaSetting().EmptyResponseNoBilling = tc.global
			resolved := ratio_setting.ResolveGroupRatio(7, "default", group, time.Now().Unix())
			require.Equal(t, 0.4, resolved.GroupRatio, "base 2 x group 0.8 x user 0.5 x schedule 0.5")
			require.True(t, resolved.HasUserRatio)
			require.True(t, resolved.HasSchedule)
			cost, trace, err := billingexpr.RunExpr(expression, billingexpr.TokenParams{})
			require.NoError(t, err)
			beforeGroup := cost / 1_000_000 * common.QuotaPerUnit
			reserve, err := billingexpr.QuotaRoundStrict(beforeGroup * resolved.GroupRatio)
			require.NoError(t, err)
			require.Equal(t, 4000, reserve)
			info := &relaycommon.RelayInfo{
				UserId: 7, TokenId: 7, TokenKey: "fixed-compat-key", UserGroup: "default", UsingGroup: group,
				OriginModelName: "fixed-compat-model", StartTime: time.Now(), FirstResponseTime: time.Now(), RelayFormat: types.RelayFormatOpenAI,
				RelayMode: relayconstant.RelayModeChatCompletions, ForcePreConsume: true,
				UserSetting:           dto.UserSetting{BillingPreference: "wallet_only"},
				ChannelMeta:           &relaycommon.ChannelMeta{ChannelId: 7, ChannelSetting: dto.ChannelSettings{EmptyResponseNoBillingEnabled: tc.channel}},
				PriceData:             hosttypes.PriceData{ModelRatio: 1, CompletionRatio: 1, CacheRatio: 0.1, GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: resolved.GroupRatio, HasSpecialRatio: resolved.HasSpecialRatio, GroupSpecialRatio: resolved.SpecialRatio, HasUserRatio: true, HasSchedule: true}},
				TieredBillingSnapshot: &billingexpr.BillingSnapshot{BillingMode: "tiered_expr", ExprString: expression, ExprHash: billingexpr.ExprHashString(expression), QuotaPerUnit: common.QuotaPerUnit, GroupRatio: resolved.GroupRatio, EstimatedQuotaBeforeGroup: beforeGroup, EstimatedQuotaAfterGroup: reserve, EstimatedTier: trace.MatchedTier, EstimatedBillingUnit: trace.BillingUnit, EstimatedFixedPrice: trace.FixedPrice},
				ResponsesUsageInfo:    &relaycommon.ResponsesUsageInfo{BuiltInTools: map[string]*relaycommon.BuildInToolInfo{"fixed_compat_tool": {CallCount: 1}}},
			}
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			require.Nil(t, PreConsumeBilling(ctx, reserve, info))
			// The selected attempt freezes its price: a concurrent setting update
			// must neither apply the user discount twice nor reprice settlement.
			require.NoError(t, ratio_setting.UpdateUserGroupRatioByJSONString(`{"7":{"fixed-compat-group":9}}`))
			ratio_setting.RefreshScheduleCache(nil)
			usage := &dto.Usage{PromptTokens: 2000, CompletionTokens: tc.completion, TotalTokens: 2000 + tc.completion, PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 1500}}
			PostTextConsumeQuota(ctx, info, usage, nil)
			want := 4800 // $0.01 x request multiplier 2 x group 0.4, plus one $4/1K tool call.
			if tc.waived {
				want = 0
			}
			require.NoError(t, info.Billing.Settle(want), "settlement is idempotent")
			info.Billing.Refund(ctx) // A completed settlement must not schedule another refund.
			var user model.User
			var token model.Token
			var channel model.Channel
			require.NoError(t, model.DB.First(&user, 7).Error)
			require.NoError(t, model.DB.First(&token, 7).Error)
			require.NoError(t, model.DB.First(&channel, 7).Error)
			assert.Equal(t, startingQuota-want, user.Quota)
			assert.Equal(t, startingQuota-want, token.RemainQuota)
			assert.Equal(t, want, user.UsedQuota)
			assert.Equal(t, want, token.UsedQuota)
			assert.EqualValues(t, want, channel.UsedQuota)
			var events []model.Log
			require.NoError(t, model.DB.Where("user_id = ?", 7).Find(&events).Error)
			require.Len(t, events, 1)
			var other map[string]any
			require.NoError(t, common.UnmarshalJsonStr(events[0].Other, &other))
			if tc.waived {
				assert.Zero(t, user.RequestCount)
				assert.Equal(t, model.LogTypeError, events[0].Type)
				assert.Equal(t, true, other["empty_response_skipped"])
			} else {
				assert.Equal(t, 1, user.RequestCount)
				assert.Equal(t, model.LogTypeConsume, events[0].Type)
				assert.Equal(t, "request", other["billing_unit"])
				assert.Equal(t, 0.01, other["fixed_price"])
			}
		})
	}
}
