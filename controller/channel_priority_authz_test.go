package controller

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"new-router/common"
	"new-router/middleware"
	"new-router/model"
	"new-router/service/authz"
)

func setupChannelPriorityAuthzTest(t *testing.T) (*gin.Engine, *model.User, string, []model.Channel) {
	t.Helper()
	user, token := setupAccessTokenAudit(t)
	sqlDB, err := model.DB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	previousMemoryCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCache
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, model.DB.AutoMigrate(&model.Channel{}, &model.Ability{}))
	setting := user.GetSetting()
	setting.AdminManagedTags = []string{"team-a"}
	user.SetSetting(setting)
	require.NoError(t, model.DB.Model(user).Update("setting", user.Setting).Error)

	teamA, teamB, empty := "team-a", "team-b", ""
	high, low, zero := int64(100), int64(99), int64(0)
	channels := []model.Channel{
		{Name: "allowed-high", Tag: &teamA, Priority: &high, Status: common.ChannelStatusEnabled},
		{Name: "allowed-low", Tag: &teamA, Priority: &low, Status: common.ChannelStatusEnabled},
		{Name: "hidden-peer", Tag: &teamB, Priority: &low, Status: common.ChannelStatusEnabled},
		{Name: "allowed-moving", Tag: &teamA, Priority: &zero, Status: common.ChannelStatusEnabled},
		{Name: "untagged", Priority: &zero, Status: common.ChannelStatusEnabled},
		{Name: "empty-tag", Tag: &empty, Priority: &zero, Status: common.ChannelStatusEnabled},
	}
	require.NoError(t, model.DB.Create(&channels).Error)
	for _, channel := range channels {
		require.NoError(t, model.DB.Create(&model.Ability{
			Group: "default", Model: "test", ChannelId: channel.Id, Priority: channel.Priority, Enabled: true,
		}).Error)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	const orderPath, reorderPath = "/api/channel/priority/order", "/api/channel/priority/reorder"
	middleware.DeclareAccessTokenPermissionRoute(http.MethodGet, orderPath, authz.ChannelRead)
	middleware.DeclareAccessTokenPermissionRoute(http.MethodPost, reorderPath, authz.ChannelWrite)
	router.GET(orderPath, middleware.AdminAuth(), middleware.RequirePermission(authz.ChannelRead), GetChannelPriorityOrder)
	router.POST(reorderPath, middleware.AdminAuth(), middleware.RequirePermission(authz.ChannelWrite), ReorderChannelPriority)
	return router, user, token, channels
}

func channelPriorityAuthzRequest(t *testing.T, router http.Handler, token string, move *model.ChannelPriorityMove) (bool, model.ChannelPriorityOrder) {
	t.Helper()
	method, path, body := http.MethodGet, "/api/channel/priority/order", ""
	if move != nil {
		method, path = http.MethodPost, "/api/channel/priority/reorder"
		encoded, err := common.Marshal(move)
		require.NoError(t, err)
		body = string(encoded)
	}
	response := accessTokenRequest(router, method, path, token, "", body)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var result struct {
		Success bool                       `json:"success"`
		Data    model.ChannelPriorityOrder `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
	return result.Success, result.Data
}

func assertChannelPriorityScope(t *testing.T, order model.ChannelPriorityOrder, channels []model.Channel) {
	t.Helper()
	var ids []int
	for _, item := range order.Items {
		ids = append(ids, item.ID)
	}
	assert.Equal(t, []int{channels[0].Id, channels[1].Id, channels[3].Id}, ids)
	for _, change := range order.Changes {
		assert.Contains(t, ids, change.ID, "every indirectly shifted priority must be manageable")
	}
}

func TestChannelPriorityRespectsManagedTags(t *testing.T) {
	router, _, token, channels := setupChannelPriorityAuthzTest(t)
	success, initial := channelPriorityAuthzRequest(t, router, token, nil)
	require.True(t, success)
	assertChannelPriorityScope(t, initial, channels)
	move := model.ChannelPriorityMove{
		Revision: initial.Revision, ID: channels[3].Id, TargetID: channels[1].Id, Position: "before", Preview: true,
	}
	success, preview := channelPriorityAuthzRequest(t, router, token, &move)
	require.True(t, success)
	assertChannelPriorityScope(t, preview, channels)
	require.Len(t, preview.Changes, 2, "an occupied slot also shifts an authorized neighbor")
	_, unchanged := channelPriorityAuthzRequest(t, router, token, nil)
	assert.Equal(t, initial, unchanged, "preview must not write priorities")

	move.Preview = false
	success, applied := channelPriorityAuthzRequest(t, router, token, &move)
	require.True(t, success)
	assert.Equal(t, preview, applied)
	for i, channel := range channels {
		var saved model.Channel
		var ability model.Ability
		require.NoError(t, model.DB.First(&saved, channel.Id).Error)
		require.NoError(t, model.DB.Where("channel_id = ?", channel.Id).First(&ability).Error)
		assert.Equal(t, saved.Priority, ability.Priority, "routing abilities must match the persisted priority")
		if i == 2 || i >= 4 {
			assert.Equal(t, channel.Priority, saved.Priority, "hidden and untagged channels must stay untouched")
		}
	}

	undo := model.ChannelPriorityMove{Revision: applied.Revision, Preview: true}
	for _, change := range applied.Changes {
		undo.Restore = append(undo.Restore, model.ChannelPriorityChange{ID: change.ID, Previous: change.Priority, Priority: change.Previous})
	}
	success, undoPreview := channelPriorityAuthzRequest(t, router, token, &undo)
	require.True(t, success)
	assert.Equal(t, initial.Items, undoPreview.Items)
	_, unchanged = channelPriorityAuthzRequest(t, router, token, nil)
	assert.Equal(t, applied.Items, unchanged.Items)
	assert.Equal(t, applied.Revision, unchanged.Revision)
	undo.Preview = false
	success, restored := channelPriorityAuthzRequest(t, router, token, &undo)
	require.True(t, success)
	assert.Equal(t, initial.Items, restored.Items)
	assert.Equal(t, initial.Revision, restored.Revision)
}

func TestChannelPriorityRejectsOutOfScopeMovesAndRestore(t *testing.T) {
	router, _, token, channels := setupChannelPriorityAuthzTest(t)
	_, initial := channelPriorityAuthzRequest(t, router, token, nil)
	for _, preview := range []bool{true, false} {
		for _, move := range []model.ChannelPriorityMove{
			{ID: channels[2].Id, TargetID: channels[0].Id, Position: "before"},
			{ID: channels[3].Id, TargetID: channels[2].Id, Position: "same"},
			{Restore: []model.ChannelPriorityChange{{ID: channels[2].Id, Previous: channels[2].Priority, Priority: channels[0].Priority}}},
			{Restore: []model.ChannelPriorityChange{
				{ID: channels[1].Id, Previous: channels[1].Priority, Priority: channels[0].Priority},
				{ID: channels[2].Id, Previous: channels[2].Priority, Priority: channels[0].Priority},
			}},
		} {
			move.Revision, move.Preview = initial.Revision, preview
			success, _ := channelPriorityAuthzRequest(t, router, token, &move)
			assert.False(t, success)
			_, unchanged := channelPriorityAuthzRequest(t, router, token, nil)
			assert.Equal(t, initial, unchanged, "rejected move or restore must be atomic")
		}
	}

	// Scope is resolved again for commit, rather than trusting preview IDs.
	move := model.ChannelPriorityMove{Revision: initial.Revision, ID: channels[3].Id, TargetID: channels[1].Id, Position: "before", Preview: true}
	success, _ := channelPriorityAuthzRequest(t, router, token, &move)
	require.True(t, success)
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", channels[3].Id).Update("tag", "team-b").Error)
	move.Preview = false
	success, _ = channelPriorityAuthzRequest(t, router, token, &move)
	assert.False(t, success, "retagging a channel after preview must invalidate the revision")
	var saved model.Channel
	require.NoError(t, model.DB.First(&saved, channels[3].Id).Error)
	assert.Equal(t, channels[3].Priority, saved.Priority)
}

func TestChannelPriorityRootAndUnrestrictedAdmin(t *testing.T) {
	router, user, token, channels := setupChannelPriorityAuthzTest(t)
	rootToken := "priority-root-token"
	root := model.User{
		Username: "priority-root", Role: common.RoleRootUser, Status: common.UserStatusEnabled,
		Group: "default", AccessToken: &rootToken, AuthVersion: 1, AffCode: "priority-root", Setting: user.Setting,
	}
	require.NoError(t, model.DB.Create(&root).Error)
	success, initial := channelPriorityAuthzRequest(t, router, rootToken, nil)
	require.True(t, success)
	require.Len(t, initial.Items, len(channels), "root ignores an old managed-tag setting")
	move := model.ChannelPriorityMove{Revision: initial.Revision, ID: channels[2].Id, TargetID: channels[0].Id, Position: "same", Preview: true}
	success, preview := channelPriorityAuthzRequest(t, router, rootToken, &move)
	require.True(t, success)
	move.Preview = false
	success, applied := channelPriorityAuthzRequest(t, router, rootToken, &move)
	require.True(t, success)
	assert.Equal(t, preview, applied)
	undo := model.ChannelPriorityMove{Revision: applied.Revision}
	for _, change := range applied.Changes {
		undo.Restore = append(undo.Restore, model.ChannelPriorityChange{ID: change.ID, Previous: change.Priority, Priority: change.Previous})
	}
	success, restored := channelPriorityAuthzRequest(t, router, rootToken, &undo)
	require.True(t, success)
	assert.Equal(t, initial.Items, restored.Items)

	setting := user.GetSetting()
	setting.AdminManagedTags = nil
	user.SetSetting(setting)
	require.NoError(t, model.DB.Model(user).Update("setting", user.Setting).Error)
	success, unrestricted := channelPriorityAuthzRequest(t, router, token, nil)
	require.True(t, success)
	assert.Equal(t, initial.Items, unrestricted.Items, "an empty administrator whitelist retains unrestricted access")
}

func TestChannelPriorityRequiresCurrentPermissionAndTokenScope(t *testing.T) {
	router, user, _, _ := setupChannelPriorityAuthzTest(t)
	readToken, _ := createScopedAccessToken(t, user.Id, 0, "channel:read")
	writeToken, _ := createScopedAccessToken(t, user.Id, 0, "channel:write")
	response := accessTokenRequest(router, http.MethodGet, "/api/channel/priority/order", writeToken, "", "")
	assert.Equal(t, http.StatusForbidden, response.Code)
	response = accessTokenRequest(router, http.MethodPost, "/api/channel/priority/reorder", readToken, "", `{}`)
	assert.Equal(t, http.StatusForbidden, response.Code)

	require.NoError(t, authz.SetUserPermissions(user.Id, authz.PermissionsMap{authz.ResourceChannel: {authz.ActionRead: false, authz.ActionWrite: false}}))
	response = accessTokenRequest(router, http.MethodGet, "/api/channel/priority/order", readToken, "", "")
	assert.Equal(t, http.StatusForbidden, response.Code, "a token scope cannot widen revoked Casbin permissions")
	response = accessTokenRequest(router, http.MethodPost, "/api/channel/priority/reorder", writeToken, "", `{}`)
	assert.Equal(t, http.StatusForbidden, response.Code)
}
