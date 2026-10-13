package model

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"new-router/common"
)

func TestCompatibilityBalancePurchaseKeepsOptInAndAtomicLedger(t *testing.T) {
	truncateTables(t)
	previousQuota := common.QuotaPerUnit
	common.QuotaPerUnit = 100
	t.Cleanup(func() { common.QuotaPerUnit = previousQuota })
	user := User{Username: "compat-balance", AffCode: "compat-balance", Quota: 1000, AuthVersion: 1}
	require.NoError(t, DB.Create(&user).Error)
	plan := SubscriptionPlan{Title: "Compatibility plan", PriceAmount: 0.5, Enabled: true, AllowBalancePay: common.GetPointer(true), MaxPurchasePerUser: 1, DurationUnit: SubscriptionDurationHour, DurationValue: 1}
	require.NoError(t, DB.Create(&plan).Error)
	require.Error(t, PurchaseSubscriptionWithBalance(user.Id, plan.Id), "legacy allow_balance_pay must not bypass opt-in")
	require.NoError(t, DB.Model(&plan).Update("allow_balance_purchase", false).Error)
	require.Error(t, PurchaseSubscriptionWithBalance(user.Id, plan.Id))
	require.NoError(t, DB.First(&user, user.Id).Error)
	assert.Equal(t, 1000, user.Quota)

	require.NoError(t, DB.Model(&plan).Update("allow_balance_purchase", true).Error)
	const callback = "test:reject-compatibility-ledger"
	require.NoError(t, DB.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "top_ups" {
			tx.AddError(errors.New("ledger unavailable"))
		}
	}))
	require.Error(t, PurchaseSubscriptionWithBalance(user.Id, plan.Id))
	require.NoError(t, DB.Callback().Create().Remove(callback))
	require.NoError(t, DB.First(&user, user.Id).Error)
	assert.Equal(t, 1000, user.Quota)
	for _, record := range []any{&SubscriptionOrder{}, &UserSubscription{}, &TopUp{}} {
		var count int64
		require.NoError(t, DB.Model(record).Count(&count).Error)
		assert.Zero(t, count)
	}

	require.NoError(t, PurchaseSubscriptionWithBalance(user.Id, plan.Id))
	require.NoError(t, DB.First(&user, user.Id).Error)
	assert.Equal(t, 950, user.Quota)
	var order SubscriptionOrder
	require.NoError(t, DB.Where("user_id = ?", user.Id).First(&order).Error)
	var payment TopUp
	require.NoError(t, DB.Where("trade_no = ?", order.TradeNo).First(&payment).Error)
	assert.Equal(t, PaymentMethodBalance, payment.PaymentMethod)
	assert.Equal(t, common.TopUpStatusSuccess, payment.Status)
	require.Error(t, PurchaseSubscriptionWithBalance(user.Id, plan.Id), "compatibility API must enforce purchase cap")
	require.NoError(t, DB.First(&user, user.Id).Error)
	assert.Equal(t, 950, user.Quota)
}
