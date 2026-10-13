package controller

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"new-router/common"
	"new-router/model"
	"new-router/service/authz"
)

func TestQuotaBatchCancellationRequiresDeletePermission(t *testing.T) {
	admin, _ := setupAccessTokenAudit(t)
	target := model.User{Username: "batch-delete-target", AffCode: "batch-delete-target", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AuthVersion: 1}
	require.NoError(t, model.DB.Create(&target).Error)
	require.NoError(t, authz.SetUserPermissions(admin.Id, authz.PermissionsMap{authz.ResourceUser: {authz.ActionManage: true, authz.ActionDelete: false}}))
	assert.True(t, authz.Can(admin.Id, admin.Role, authz.UserManage))
	assert.False(t, authz.Can(admin.Id, admin.Role, authz.UserDelete))
	ctx, recorder := newAuthenticatedContext(t, http.MethodPost, "/api/user/batch/quota_action", map[string]any{"action": "cancel", "remaining_quota_min": 0}, admin.Id)
	ctx.Set("role", admin.Role)
	BatchUserActionByQuota(ctx)
	response := decodeAPIResponse(t, recorder)
	assert.False(t, response.Success)
	require.NoError(t, model.DB.First(&target, target.Id).Error, "denied cancellation must retain the user")

	require.NoError(t, authz.SetUserPermissions(admin.Id, authz.PermissionsMap{authz.ResourceUser: {authz.ActionManage: true, authz.ActionDelete: true}}))
	// A scoped token cannot inherit delete access merely because its owner has it.
	ctx, recorder = newAuthenticatedContext(t, http.MethodPost, "/api/user/batch/quota_action", map[string]any{"action": "cancel", "remaining_quota_min": 0}, admin.Id)
	ctx.Set("role", admin.Role)
	ctx.Set("use_access_token", true)
	ctx.Set("access_token_scopes", []string{"user:manage"})
	BatchUserActionByQuota(ctx)
	assert.Equal(t, http.StatusForbidden, recorder.Code)
	assert.False(t, decodeAPIResponse(t, recorder).Success)
	require.NoError(t, model.DB.First(&target, target.Id).Error)

	ctx, recorder = newAuthenticatedContext(t, http.MethodPost, "/api/user/batch/quota_action", map[string]any{"action": "cancel", "remaining_quota_min": 0}, admin.Id)
	ctx.Set("role", admin.Role)
	ctx.Set("use_access_token", true)
	ctx.Set("access_token_scopes", []string{"user:manage", "user:delete"})
	BatchUserActionByQuota(ctx)
	assert.True(t, decodeAPIResponse(t, recorder).Success)
	var stored model.User
	require.NoError(t, model.DB.Unscoped().First(&stored, target.Id).Error)
	assert.True(t, stored.DeletedAt.Valid)
}
