package controller

import (
	"net/http"

	"new-router/common"
	"new-router/middleware"
	"new-router/model"
	"new-router/service"

	"github.com/gin-gonic/gin"
)

func ClearAuditLogs(c *gin.Context) {
	if c.GetInt("role") != common.RoleRootUser || c.GetBool("use_access_token") {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "message": "Administrator privilege required"})
		return
	}
	var request service.AuditLogClearContext
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorT(c, "Select a valid timestamp before clearing audit logs.")
		return
	}
	context, err := common.Marshal(request)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	operation := service.VerificationOperation{Scope: service.VerificationScopeAuditLogClear, Context: context}
	if _, err := service.BindVerificationOperation(operation); err != nil {
		common.ApiErrorT(c, "Select a valid timestamp before clearing audit logs.")
		return
	}
	if middleware.RequireSecurityProof(c, operation) == nil {
		return
	}
	deleted, err := model.ClearAuditLogsBeforeWithAudit(c.Request.Context(), request.TargetTimestamp, model.AuditLog{
		UserId: c.GetInt("id"), Username: c.GetString("username"),
		RequestId: c.GetString(common.RequestIdKey), Ip: c.ClientIP(),
		UserAgent: c.Request.UserAgent(), Method: c.Request.Method, Route: c.FullPath(),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// The model persists a completion event even when operation auditing is off.
	markAuditLogged(c)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"deleted_count": deleted}})
}
