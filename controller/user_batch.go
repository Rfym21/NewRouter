package controller

import (
	"fmt"

	"new-router/common"
	"new-router/i18n"
	"new-router/middleware"
	"new-router/model"
	"new-router/service"
	"new-router/service/authz"

	"github.com/gin-gonic/gin"
)

type batchUserActionRequest struct {
	Action            string `json:"action"`
	UsedQuotaMin      *int   `json:"used_quota_min,omitempty"`
	UsedQuotaMax      *int   `json:"used_quota_max,omitempty"`
	RemainingQuotaMin *int   `json:"remaining_quota_min,omitempty"`
	RemainingQuotaMax *int   `json:"remaining_quota_max,omitempty"`
	TotalQuotaMin     *int   `json:"total_quota_min,omitempty"`
	TotalQuotaMax     *int   `json:"total_quota_max,omitempty"`
	DryRun            bool   `json:"dry_run,omitempty"`
}

func (request batchUserActionRequest) filter() model.BatchUserActionFilter {
	return model.BatchUserActionFilter{
		UsedQuotaMin:      request.UsedQuotaMin,
		UsedQuotaMax:      request.UsedQuotaMax,
		RemainingQuotaMin: request.RemainingQuotaMin,
		RemainingQuotaMax: request.RemainingQuotaMax,
		TotalQuotaMin:     request.TotalQuotaMin,
		TotalQuotaMax:     request.TotalQuotaMax,
	}
}

func (request batchUserActionRequest) validate() error {
	if !request.DryRun && request.Action != "disable" && request.Action != "cancel" {
		return fmt.Errorf("action 必须为 disable 或 cancel")
	}
	if !request.filter().HasConditions() {
		return fmt.Errorf("至少需要指定一个筛选条件")
	}
	ranges := []struct {
		name string
		min  *int
		max  *int
	}{
		{name: "used_quota", min: request.UsedQuotaMin, max: request.UsedQuotaMax},
		{name: "remaining_quota", min: request.RemainingQuotaMin, max: request.RemainingQuotaMax},
		{name: "total_quota", min: request.TotalQuotaMin, max: request.TotalQuotaMax},
	}
	for _, quotaRange := range ranges {
		if quotaRange.min != nil && quotaRange.max != nil && *quotaRange.min > *quotaRange.max {
			return fmt.Errorf("%s 的最小值不能大于最大值", quotaRange.name)
		}
	}
	return nil
}

func (request batchUserActionRequest) auditParams() map[string]interface{} {
	params := map[string]interface{}{}
	boundaries := []struct {
		name  string
		value *int
	}{
		{name: "used_quota_min", value: request.UsedQuotaMin},
		{name: "used_quota_max", value: request.UsedQuotaMax},
		{name: "remaining_quota_min", value: request.RemainingQuotaMin},
		{name: "remaining_quota_max", value: request.RemainingQuotaMax},
		{name: "total_quota_min", value: request.TotalQuotaMin},
		{name: "total_quota_max", value: request.TotalQuotaMax},
	}
	for _, boundary := range boundaries {
		if boundary.value != nil {
			params[boundary.name] = *boundary.value
		}
	}
	return params
}

// PurgeDeletedUsers permanently removes soft-deleted non-root user records.
func PurgeDeletedUsers(c *gin.Context) {
	count, err := model.PurgeSoftDeletedUsers()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "user.purge_deleted", map[string]interface{}{
		"deleted": count,
	})
	common.ApiSuccess(c, gin.H{"deleted": count})
}

// BatchUserActionByQuota previews or applies a bulk disable/cancel operation to ordinary users.
func BatchUserActionByQuota(c *gin.Context) {
	var request batchUserActionRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	if err := request.validate(); err != nil {
		common.ApiError(c, err)
		return
	}

	if !request.DryRun && request.Action == "cancel" {
		if c.GetBool("use_access_token") && !c.GetBool("access_token_legacy") {
			scopes, _ := c.Get("access_token_scopes")
			granted, _ := scopes.([]string)
			deleteScope := service.AccessTokenScopeOf(authz.UserDelete)
			if !service.AccessTokenScopeGranted(granted, deleteScope) {
				common.ApiErrorStatus(c, 403, common.NewMessage("The access token lacks the required permission: {{scope}}", map[string]any{"scope": deleteScope}), gin.H{"code": "ACCESS_TOKEN_SCOPE_DENIED"})
				return
			}
		}
		if !middleware.PermissionAllowed(c, authz.UserDelete) {
			common.ApiErrorI18n(c, i18n.MsgAuthInsufficientPrivilege)
			return
		}
	}

	filter := request.filter()
	matched, err := model.CountUsersByQuotaFilter(filter)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	params := request.auditParams()
	params["matched"] = matched
	if request.DryRun {
		recordManageAudit(c, "user.batch_quota_preview", params)
		common.ApiSuccess(c, gin.H{"matched": matched})
		return
	}

	var affected int64
	switch request.Action {
	case "disable":
		affected, err = model.BatchDisableUsersByQuotaFilter(filter)
	case "cancel":
		affected, err = model.BatchSoftDeleteUsersByQuotaFilter(filter)
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	params["action"] = request.Action
	params["affected"] = affected
	recordManageAudit(c, "user.batch_quota_action", params)
	common.ApiSuccess(c, gin.H{"matched": matched, "affected": affected})
}
