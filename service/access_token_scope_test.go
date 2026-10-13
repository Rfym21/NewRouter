package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"new-router/common"
	"new-router/service/authz"
)

func TestAccessTokenScopeCatalogUsesLocalAuthorizationActions(t *testing.T) {
	// Root needs no policy database, so this checks the complete grant catalog.
	seen := make(map[string]bool)
	for _, group := range AccessTokenCatalog(1, common.RoleRootUser) {
		for _, resource := range group.Resources {
			require.False(t, seen[resource.Resource], "duplicate grant resource %s", resource.Resource)
			seen[resource.Resource] = true
			if resource.Resource == authz.ResourceUser {
				actions := make([]string, 0, len(resource.Actions))
				for _, action := range resource.Actions {
					actions = append(actions, action.Action)
				}
				assert.ElementsMatch(t, []string{authz.ActionRead, authz.ActionWrite, authz.ActionManage, authz.ActionDelete}, actions)
			}
		}
	}
	assert.True(t, seen[authz.ResourceUser])
	seen = make(map[string]bool)
	for _, resource := range AccessTokenScopeDictionary() {
		assert.False(t, seen[resource.Resource], "duplicate scope dictionary resource %s", resource.Resource)
		seen[resource.Resource] = true
	}
	for _, scope := range []string{"user:manage", "user:delete", "redemption:delete"} {
		assert.True(t, knownAccessTokenScope(scope), scope)
	}
	assert.False(t, knownAccessTokenScope("user:reveal"))
}

func TestAccessTokenScopeCannotGrantPrivilegedUserActionsFromStaticCatalog(t *testing.T) {
	// Even without an initialized policy database, the static catalog must not
	// confer admin grants that authz denies.
	for _, scope := range []string{"user:write", "user:manage", "user:delete"} {
		_, err := NormalizeAccessTokenScopes(123456, common.RoleAdminUser, []string{scope})
		assert.ErrorIs(t, err, ErrAccessTokenScopeForbidden, scope)
	}
	for verification, permission := range map[string]authz.Permission{
		VerificationScopeAdminUserDelete:       authz.UserDelete,
		VerificationScopeAdminUserManage:       authz.UserManage,
		VerificationScopeAdminUserPasskeyReset: authz.UserManage,
		VerificationScopeAdminUserTwoFADisable: authz.UserManage,
		VerificationScopeAdminUserBindingClear: authz.UserManage,
	} {
		scope, found := AccessTokenVerificationScope(verification)
		assert.True(t, found)
		assert.Equal(t, AccessTokenScopeOf(permission), scope)
	}
}
