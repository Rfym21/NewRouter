package controller

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"new-router/common"
	"new-router/setting/ratio_setting"
)

func TestLegacyPricingOptionPublishesOnlyAfterCommit(t *testing.T) {
	database := modelManagementDB(t, "sqlite", "")
	require.NoError(t, ratio_setting.UpdateImageRatioByJSONString(`{"atomic-model":2}`))
	const callback = "test:reject-pricing-write"
	require.NoError(t, database.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "options" {
			tx.AddError(errors.New("simulated pricing write failure"))
		}
	}))
	t.Cleanup(func() { require.NoError(t, database.Callback().Update().Remove(callback)) })
	response := modelManagementRequest(t, UpdateOption, http.MethodPut, "/api/option/", OptionUpdateRequest{Key: "ImageRatio", Value: `{"atomic-model":7}`}, nil)
	assert.Contains(t, response.Body.String(), `"success":false`)
	live, exists := ratio_setting.GetImageRatio("atomic-model")
	assert.True(t, exists)
	assert.Equal(t, float64(2), live)
	common.OptionMapRWMutex.RLock()
	persistedOption := common.OptionMap["ImageRatio"]
	common.OptionMapRWMutex.RUnlock()
	assert.NotContains(t, persistedOption, "7")
}
