package service

import (
	"errors"
	"fmt"
	"math"
	"time"

	"new-router/common"
	"new-router/model"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

var (
	// ErrSubscriptionBalancePlanDisabled indicates that the selected plan is disabled.
	ErrSubscriptionBalancePlanDisabled = errors.New("subscription balance plan disabled")
	// ErrSubscriptionBalancePurchaseDisabled indicates that the plan has not opted in to balance purchases.
	ErrSubscriptionBalancePurchaseDisabled = errors.New("subscription balance purchase disabled")
	// ErrSubscriptionBalancePriceInvalid indicates that the plan price cannot be converted safely to quota.
	ErrSubscriptionBalancePriceInvalid = errors.New("subscription balance price invalid")
	// ErrSubscriptionBalancePurchaseLimited indicates that the user reached the plan purchase limit.
	ErrSubscriptionBalancePurchaseLimited = errors.New("subscription balance purchase limited")
	// ErrSubscriptionBalanceInsufficient indicates that the account quota cannot cover the purchase.
	ErrSubscriptionBalanceInsufficient = errors.New("subscription balance insufficient")
	// ErrSubscriptionBalanceOrderCreate indicates that the payment audit records could not be created.
	ErrSubscriptionBalanceOrderCreate = errors.New("subscription balance order create failed")
)

// PurchaseSubscriptionWithBalance atomically deducts account quota and creates
// the completed subscription payment records.
func PurchaseSubscriptionWithBalance(userId int, planId int) (string, error) {
	if userId <= 0 || planId <= 0 {
		return "", errors.New("invalid subscription balance request")
	}

	tradeNo := fmt.Sprintf("SUBUSR%dBAL%s%d", userId, common.GetRandomString(6), time.Now().UnixNano())
	var planTitle string
	var priceAmount float64
	var chargedQuota int
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		plan, err := model.GetSubscriptionPlanByIdForUpdateTx(tx, planId)
		if err != nil {
			return err
		}
		if !plan.Enabled {
			return ErrSubscriptionBalancePlanDisabled
		}
		if plan.AllowBalancePurchase == nil || !*plan.AllowBalancePurchase {
			return ErrSubscriptionBalancePurchaseDisabled
		}

		chargedQuota, err = calculateSubscriptionBalanceQuota(plan.PriceAmount)
		if err != nil {
			return err
		}
		if err := deductUserQuotaIfSufficientTx(tx, userId, chargedQuota); err != nil {
			return err
		}

		if plan.MaxPurchasePerUser > 0 {
			var count int64
			if err := tx.Model(&model.UserSubscription{}).
				Where("user_id = ? AND plan_id = ?", userId, plan.Id).
				Count(&count).Error; err != nil {
				return err
			}
			if count >= int64(plan.MaxPurchasePerUser) {
				return ErrSubscriptionBalancePurchaseLimited
			}
		}

		if _, err := model.CreateUserSubscriptionFromPlanTx(tx, userId, plan, "order"); err != nil {
			return err
		}

		now := common.GetTimestamp()
		order := &model.SubscriptionOrder{
			UserId:          userId,
			PlanId:          plan.Id,
			Money:           plan.PriceAmount,
			TradeNo:         tradeNo,
			PaymentMethod:   model.PaymentMethodBalance,
			PaymentProvider: model.PaymentProviderBalance,
			Status:          common.TopUpStatusSuccess,
			CreateTime:      now,
			CompleteTime:    now,
			ProviderPayload: fmt.Sprintf("charged_quota=%d", chargedQuota),
		}
		if err := tx.Create(order).Error; err != nil {
			return fmt.Errorf("%w: create subscription order: %w", ErrSubscriptionBalanceOrderCreate, err)
		}
		if err := model.UpsertSubscriptionTopUpTx(tx, order); err != nil {
			return fmt.Errorf("%w: create subscription payment ledger: %w", ErrSubscriptionBalanceOrderCreate, err)
		}

		planTitle = plan.Title
		priceAmount = plan.PriceAmount
		return nil
	})
	if err != nil {
		return "", err
	}

	if err := model.SyncSubscriptionBalancePurchaseCache(userId, chargedQuota); err != nil {
		common.SysLog(common.LogText("failed to synchronize user cache after subscription balance purchase: %s", err.Error()))
	}
	model.RecordLog(userId, model.LogTypeTopup, common.NewMessage(
		"Purchased subscription with balance: plan {{plan}}, amount {{amount}}, deducted quota {{quota}}",
		map[string]any{"plan": planTitle, "amount": fmt.Sprintf("%.2f", priceAmount), "quota": chargedQuota},
	))
	return tradeNo, nil
}

func calculateSubscriptionBalanceQuota(priceAmount float64) (int, error) {
	if priceAmount < 0 || math.IsNaN(priceAmount) || math.IsInf(priceAmount, 0) {
		return 0, ErrSubscriptionBalancePriceInvalid
	}
	if priceAmount == 0 {
		return 0, nil
	}
	if common.QuotaPerUnit <= 0 || math.IsNaN(common.QuotaPerUnit) || math.IsInf(common.QuotaPerUnit, 0) {
		return 0, ErrSubscriptionBalancePriceInvalid
	}

	quota, clamp := common.QuotaFromDecimalChecked(
		decimal.NewFromFloat(priceAmount).
			Mul(decimal.NewFromFloat(common.QuotaPerUnit)).
			Ceil(),
	)
	if clamp != nil || quota < 0 {
		return 0, ErrSubscriptionBalancePriceInvalid
	}
	return quota, nil
}

func deductUserQuotaIfSufficientTx(tx *gorm.DB, userId int, quota int) error {
	if tx == nil {
		return errors.New("tx is nil")
	}
	if quota < 0 {
		return ErrSubscriptionBalancePriceInvalid
	}

	res := tx.Model(&model.User{}).
		Where("id = ? AND quota >= ?", userId, quota).
		UpdateColumn("quota", gorm.Expr("quota - ?", quota))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		return nil
	}
	if quota > 0 {
		return ErrSubscriptionBalanceInsufficient
	}

	var count int64
	if err := tx.Model(&model.User{}).Where("id = ?", userId).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
