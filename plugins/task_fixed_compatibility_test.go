package plugins_test

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"new-router/common"
	"new-router/constant"
	"new-router/pkg/jsplugin"
	"new-router/relay"
	relaycommon "new-router/relay/common"
	"new-router/setting/config"
	"new-router/setting/ratio_setting"
)

type reservedTaskBilling struct{}

func (reservedTaskBilling) Settle(int) error         { return nil }
func (reservedTaskBilling) Refund(*gin.Context)      {}
func (reservedTaskBilling) NeedsRefund() bool        { return false }
func (reservedTaskBilling) GetPreConsumedQuota() int { return 1 }
func (reservedTaskBilling) Reserve(int) error        { return nil }

func TestTaskFixedExpressionRestrictionPreservesLegacyPerRequestPrice(t *testing.T) {
	const modelName = "wan2.7-t2v"
	savedConfig := make(map[string]string)
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error { savedConfig[key] = value; return nil }))
	savedPrices := ratio_setting.ModelPrice2JSONString()
	savedPatches := append([]string(nil), constant.TaskPricePatches...)
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(savedConfig))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedPrices))
		constant.TaskPricePatches = savedPatches
	})
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"wan2.7-t2v":0.25}`))
	constant.TaskPricePatches = append(constant.TaskPricePatches, modelName)
	plugin := newAlibabaPlugin(t)
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		require.Equal(t, "/api/v1/services/aigc/video-generation/video-synthesis", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":{"task_id":"existing-per-request-price"}}`))
	}))
	defer upstream.Close()
	newContext := func() (*gin.Context, *relaycommon.RelayInfo) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
		c.Request.Header.Set("Content-Type", "application/json")
		common.SetContextKey(c, constant.ContextKeyOriginalModel, modelName)
		common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
		c.Set("channel_type", constant.ChannelTypeAli)
		c.Set("task_request", map[string]any{"model": modelName, "prompt": "a cat", "duration": 5})
		c.Set(jsplugin.ContextKeyPinnedPlugin, jsplugin.PinnedPlugin{Plugin: plugin})
		return c, &relaycommon.RelayInfo{OriginModelName: modelName, UserGroup: "default", UsingGroup: "default", TaskRelayInfo: &relaycommon.TaskRelayInfo{}, Billing: reservedTaskBilling{}}
	}

	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"billing_setting.billing_mode": `{}`, "billing_setting.billing_expr": `{}`}))
	c, info := newContext()
	result, taskErr := relay.RelayTaskSubmit(c, info)
	require.Nil(t, taskErr)
	require.NotNil(t, result)
	require.Equal(t, int(0.25*common.QuotaPerUnit), result.Quota)
	require.Equal(t, int32(1), requests.Load())
	require.Nil(t, info.TieredBillingSnapshot)

	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode": `{"wan2.7-t2v":"tiered_expr"}`,
		"billing_setting.billing_expr": `{"wan2.7-t2v":"tier(\"fixed\", fixed(0.25))"}`,
	}))
	c, info = newContext()
	result, taskErr = relay.RelayTaskSubmit(c, info)
	require.Nil(t, result)
	require.NotNil(t, taskErr)
	require.Equal(t, "model_price_error", taskErr.Code)
	require.ErrorContains(t, taskErr.Error, "fixed pricing is not supported for task usage expressions")
	require.Equal(t, int32(1), requests.Load(), "unsupported expressions must fail before another upstream submission")
}
