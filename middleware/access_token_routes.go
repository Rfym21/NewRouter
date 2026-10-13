package middleware

import (
	"fmt"
	"maps"
	"slices"
	"sync"

	"new-router/service"
	"new-router/service/authz"
)

type accessTokenRuleKind int

const (
	// accessTokenRuleScope requires the token to carry scope.
	accessTokenRuleScope accessTokenRuleKind = iota + 1
	// accessTokenRuleAny accepts every valid scoped token; the handler applies
	// its own checks.
	accessTokenRuleAny
	// accessTokenRuleSession rejects every access token, legacy ones included.
	accessTokenRuleSession
)

type accessTokenRouteRule struct {
	kind  accessTokenRuleKind
	scope string
}

var (
	accessTokenAnyRule     = accessTokenRouteRule{kind: accessTokenRuleAny}
	accessTokenSessionRule = accessTokenRouteRule{kind: accessTokenRuleSession}
)

func accessTokenScopeRule(scope string) accessTokenRouteRule {
	return accessTokenRouteRule{kind: accessTokenRuleScope, scope: scope}
}

// accessTokenRouteRules declares the token rule of every dashboard route that
// is not guarded by RequirePermission, keyed by method and gin full path.
// Casbin-guarded routes declare themselves through
// DeclareAccessTokenPermissionRoute when they are registered.
var accessTokenRouteRules = map[string]accessTokenRouteRule{
	// router/api-router.go: top level
	"GET /api/models":                     accessTokenScopeRule("profile:read"),
	"GET /api/status/test":                accessTokenScopeRule("log:read"),
	"GET /api/pricing":                    accessTokenAnyRule,
	"GET /api/perf-metrics/summary":       accessTokenAnyRule,
	"GET /api/perf-metrics":               accessTokenAnyRule,
	"GET /api/rankings":                   accessTokenAnyRule,
	"POST /api/oauth/state":               accessTokenSessionRule,
	"POST /api/oauth/email/bind/start":    accessTokenScopeRule("account_security:write"),
	"POST /api/oauth/email/bind/resend":   accessTokenScopeRule("account_security:write"),
	"POST /api/oauth/email/bind":          accessTokenScopeRule("account_security:write"),
	"POST /api/oauth/wechat/bind":         accessTokenScopeRule("account_security:write"),
	"POST /api/oauth/telegram/bind/start": accessTokenSessionRule,
	"GET /api/oauth/:provider":            accessTokenSessionRule,
	"GET /api/verify/methods":             accessTokenAnyRule,
	"POST /api/verify":                    accessTokenAnyRule,

	// router/api-router.go: /api/user (self)
	"GET /api/user/sessions":                       accessTokenSessionRule,
	"DELETE /api/user/sessions/:sid":               accessTokenSessionRule,
	"POST /api/user/sessions/revoke-others":        accessTokenSessionRule,
	"GET /api/user/self/groups":                    accessTokenScopeRule("profile:read"),
	"GET /api/user/self":                           accessTokenScopeRule("profile:read"),
	"GET /api/user/models":                         accessTokenScopeRule("profile:read"),
	"PUT /api/user/self":                           accessTokenScopeRule("profile:write"),
	"DELETE /api/user/self":                        accessTokenScopeRule("account_security:write"),
	"GET /api/user/access_tokens":                  accessTokenSessionRule,
	"GET /api/user/access_tokens/catalog":          accessTokenSessionRule,
	"GET /api/user/access_tokens/scopes":           accessTokenSessionRule,
	"POST /api/user/access_tokens":                 accessTokenSessionRule,
	"PATCH /api/user/access_tokens/:id":            accessTokenSessionRule,
	"DELETE /api/user/access_tokens/:id":           accessTokenSessionRule,
	"DELETE /api/user/access_tokens/legacy":        accessTokenSessionRule,
	"GET /api/user/passkey":                        accessTokenScopeRule("account_security:read"),
	"POST /api/user/passkey/register/begin":        accessTokenScopeRule("account_security:write"),
	"POST /api/user/passkey/register/finish":       accessTokenScopeRule("account_security:write"),
	"POST /api/user/passkey/verify/begin":          accessTokenAnyRule,
	"POST /api/user/passkey/verify/finish":         accessTokenAnyRule,
	"DELETE /api/user/passkey":                     accessTokenScopeRule("account_security:write"),
	"GET /api/user/aff":                            accessTokenScopeRule("wallet:read"),
	"GET /api/user/topup/info":                     accessTokenScopeRule("wallet:read"),
	"GET /api/user/topup/self":                     accessTokenScopeRule("wallet:read"),
	"POST /api/user/topup":                         accessTokenScopeRule("wallet:write"),
	"POST /api/user/pay":                           accessTokenScopeRule("wallet:write"),
	"POST /api/user/amount":                        accessTokenScopeRule("wallet:read"),
	"POST /api/user/stripe/pay":                    accessTokenScopeRule("wallet:write"),
	"POST /api/user/stripe/amount":                 accessTokenScopeRule("wallet:read"),
	"POST /api/user/creem/pay":                     accessTokenScopeRule("wallet:write"),
	"POST /api/user/waffo/amount":                  accessTokenScopeRule("wallet:read"),
	"POST /api/user/waffo/pay":                     accessTokenScopeRule("wallet:write"),
	"POST /api/user/waffo-pancake/amount":          accessTokenScopeRule("wallet:read"),
	"POST /api/user/waffo-pancake/pay":             accessTokenScopeRule("wallet:write"),
	"POST /api/user/aff_transfer":                  accessTokenScopeRule("wallet:write"),
	"PUT /api/user/setting":                        accessTokenScopeRule("profile:write"),
	"GET /api/user/2fa/status":                     accessTokenScopeRule("account_security:read"),
	"POST /api/user/2fa/setup":                     accessTokenScopeRule("account_security:write"),
	"POST /api/user/2fa/enable":                    accessTokenScopeRule("account_security:write"),
	"POST /api/user/2fa/disable":                   accessTokenScopeRule("account_security:write"),
	"POST /api/user/2fa/backup_codes":              accessTokenScopeRule("account_security:write"),
	"GET /api/user/checkin":                        accessTokenScopeRule("wallet:read"),
	"POST /api/user/checkin":                       accessTokenScopeRule("wallet:write"),
	"GET /api/user/oauth/bindings":                 accessTokenScopeRule("account_security:read"),
	"DELETE /api/user/oauth/bindings/:provider_id": accessTokenScopeRule("account_security:write"),

	// router/api-router.go: /api/user (admin)
	"GET /api/user/":                                   accessTokenScopeRule(service.AccessTokenScopeOf(authz.UserRead)),
	"GET /api/user/topup":                              accessTokenScopeRule(service.AccessTokenScopeOf(authz.UserRead)),
	"POST /api/user/topup/complete":                    accessTokenScopeRule(service.AccessTokenScopeOf(authz.UserManage)),
	"GET /api/user/search":                             accessTokenScopeRule(service.AccessTokenScopeOf(authz.UserRead)),
	"GET /api/user/:id/oauth/bindings":                 accessTokenScopeRule(service.AccessTokenScopeOf(authz.UserRead)),
	"DELETE /api/user/:id/oauth/bindings/:provider_id": accessTokenScopeRule(service.AccessTokenScopeOf(authz.UserManage)),
	"DELETE /api/user/:id/bindings/:binding_type":      accessTokenScopeRule(service.AccessTokenScopeOf(authz.UserManage)),
	"GET /api/user/:id":                                accessTokenScopeRule(service.AccessTokenScopeOf(authz.UserRead)),
	"POST /api/user/":                                  accessTokenScopeRule(service.AccessTokenScopeOf(authz.UserWrite)),
	"POST /api/user/manage":                            accessTokenScopeRule(service.AccessTokenScopeOf(authz.UserManage)),
	"PUT /api/user/":                                   accessTokenScopeRule(service.AccessTokenScopeOf(authz.UserWrite)),
	"DELETE /api/user/:id":                             accessTokenScopeRule(service.AccessTokenScopeOf(authz.UserDelete)),
	"DELETE /api/user/:id/reset_passkey":               accessTokenScopeRule(service.AccessTokenScopeOf(authz.UserManage)),
	"GET /api/user/2fa/stats":                          accessTokenScopeRule(service.AccessTokenScopeOf(authz.UserRead)),
	"DELETE /api/user/:id/2fa":                         accessTokenScopeRule(service.AccessTokenScopeOf(authz.UserManage)),

	// router/api-router.go: /api/subscription
	"GET /api/subscription/plans":                                    accessTokenScopeRule("wallet:read"),
	"GET /api/subscription/self":                                     accessTokenScopeRule("wallet:read"),
	"PUT /api/subscription/self/preference":                          accessTokenScopeRule("wallet:write"),
	"POST /api/subscription/balance/pay":                             accessTokenScopeRule("wallet:write"),
	"POST /api/subscription/epay/pay":                                accessTokenScopeRule("wallet:write"),
	"POST /api/subscription/stripe/pay":                              accessTokenScopeRule("wallet:write"),
	"POST /api/subscription/creem/pay":                               accessTokenScopeRule("wallet:write"),
	"POST /api/subscription/waffo-pancake/pay":                       accessTokenScopeRule("wallet:write"),
	"GET /api/subscription/admin/plans":                              accessTokenScopeRule(service.AccessTokenScopeOf(authz.SubscriptionRead)),
	"POST /api/subscription/admin/plans":                             accessTokenScopeRule(service.AccessTokenScopeOf(authz.SubscriptionWrite)),
	"PUT /api/subscription/admin/plans/:id":                          accessTokenScopeRule(service.AccessTokenScopeOf(authz.SubscriptionWrite)),
	"PATCH /api/subscription/admin/plans/:id":                        accessTokenScopeRule(service.AccessTokenScopeOf(authz.SubscriptionWrite)),
	"POST /api/subscription/admin/bind":                              accessTokenScopeRule(service.AccessTokenScopeOf(authz.SubscriptionWrite)),
	"POST /api/subscription/admin/plans/:id/subscriptions/reset":     accessTokenScopeRule(service.AccessTokenScopeOf(authz.SubscriptionWrite)),
	"GET /api/subscription/admin/users/:id/subscriptions":            accessTokenScopeRule(service.AccessTokenScopeOf(authz.SubscriptionRead)),
	"POST /api/subscription/admin/users/:id/subscriptions":           accessTokenScopeRule(service.AccessTokenScopeOf(authz.SubscriptionWrite)),
	"POST /api/subscription/admin/users/:id/subscriptions/reset":     accessTokenScopeRule(service.AccessTokenScopeOf(authz.SubscriptionWrite)),
	"POST /api/subscription/admin/user_subscriptions/:id/invalidate": accessTokenScopeRule(service.AccessTokenScopeOf(authz.SubscriptionDelete)),
	"DELETE /api/subscription/admin/user_subscriptions/:id":          accessTokenScopeRule(service.AccessTokenScopeOf(authz.SubscriptionDelete)),

	// router/api-router.go: /api/option
	"GET /api/option/":                                           accessTokenScopeRule("option:read"),
	"GET /api/option/request_policy":                             accessTokenScopeRule("option:read"),
	"PATCH /api/option/request_policy":                           accessTokenScopeRule("option:write"),
	"PUT /api/option/":                                           accessTokenScopeRule("option:write"),
	"PUT /api/option/passkey/domains":                            accessTokenScopeRule("option:write"),
	"GET /api/option/model_pricing":                              accessTokenScopeRule("option:read"),
	"PATCH /api/option/model_pricing":                            accessTokenScopeRule("option:write"),
	"POST /api/option/model_pricing/convert":                     accessTokenScopeRule("option:read"),
	"POST /api/option/model_pricing/preview":                     accessTokenScopeRule("option:read"),
	"GET /api/option/channel_affinity_cache":                     accessTokenScopeRule("option:read"),
	"DELETE /api/option/channel_affinity_cache":                  accessTokenScopeRule("option:write"),
	"POST /api/option/rest_model_ratio":                          accessTokenScopeRule("option:write"),
	"GET /api/option/waffo-pancake/catalog":                      accessTokenScopeRule("option:read"),
	"POST /api/option/waffo-pancake/pair":                        accessTokenScopeRule("option:write"),
	"POST /api/option/waffo-pancake/save":                        accessTokenScopeRule("option:write"),
	"POST /api/option/waffo-pancake/subscription-product":        accessTokenScopeRule("option:write"),
	"GET /api/option/waffo-pancake/subscription-product-options": accessTokenScopeRule("option:read"),

	// router/api-router.go: /api/custom-oauth-provider, /api/ratio_sync
	"POST /api/custom-oauth-provider/discovery": accessTokenScopeRule("option:write"),
	"GET /api/custom-oauth-provider/":           accessTokenScopeRule("option:read"),
	"GET /api/custom-oauth-provider/:id":        accessTokenScopeRule("option:read"),
	"POST /api/custom-oauth-provider/":          accessTokenScopeRule("option:write"),
	"PUT /api/custom-oauth-provider/:id":        accessTokenScopeRule("option:write"),
	"DELETE /api/custom-oauth-provider/:id":     accessTokenScopeRule("option:write"),
	"GET /api/ratio_sync/channels":              accessTokenScopeRule("option:read"),
	"POST /api/ratio_sync/fetch":                accessTokenScopeRule("option:write"),

	// router/api-router.go: /api/performance, /api/system-task, /api/system-info
	"GET /api/performance/stats":                   accessTokenScopeRule("ops:read"),
	"DELETE /api/performance/disk_cache":           accessTokenScopeRule("ops:write"),
	"POST /api/performance/reset_stats":            accessTokenScopeRule("ops:write"),
	"POST /api/performance/gc":                     accessTokenScopeRule("ops:write"),
	"GET /api/performance/logs":                    accessTokenScopeRule("ops:read"),
	"DELETE /api/performance/logs":                 accessTokenScopeRule("ops:write"),
	"POST /api/system-task/log-cleanup":            accessTokenScopeRule("ops:write"),
	"GET /api/system-task/list":                    accessTokenScopeRule("ops:read"),
	"DELETE /api/system-task/history":              accessTokenScopeRule("ops:write"),
	"GET /api/system-task/current":                 accessTokenScopeRule("ops:read"),
	"GET /api/system-task/:task_id":                accessTokenScopeRule("ops:read"),
	"GET /api/system-info/instances":               accessTokenScopeRule("ops:read"),
	"DELETE /api/system-info/stale-instances":      accessTokenScopeRule("ops:write"),
	"DELETE /api/system-info/instances/:node_name": accessTokenScopeRule("ops:write"),

	// router/api-router.go: /api/plugin/task
	"GET /api/plugin/task":                           accessTokenScopeRule("plugin:read"),
	"POST /api/plugin/task":                          accessTokenScopeRule("plugin:write"),
	"PUT /api/plugin/task":                           accessTokenScopeRule("plugin:write"),
	"GET /api/plugin/task/runtime/status":            accessTokenScopeRule("plugin:read"),
	"GET /api/plugin/task/marketplace/sources":       accessTokenScopeRule("plugin:read"),
	"PUT /api/plugin/task/marketplace/sources":       accessTokenScopeRule("plugin:write"),
	"GET /api/plugin/task/:key":                      accessTokenScopeRule("plugin:read"),
	"GET /api/plugin/task/:key/icon":                 accessTokenScopeRule("plugin:read"),
	"GET /api/plugin/task/:key/versions":             accessTokenScopeRule("plugin:read"),
	"POST /api/plugin/task/:key/activate":            accessTokenScopeRule("plugin:write"),
	"POST /api/plugin/task/:key/status":              accessTokenScopeRule("plugin:write"),
	"POST /api/plugin/task/:key/dryrun":              accessTokenScopeRule("plugin:write"),
	"DELETE /api/plugin/task/:key/versions/:version": accessTokenScopeRule("plugin:write"),

	// router/channel-router.go: the key route is RootAuth, not Casbin-guarded.
	"POST /api/channel/:id/key": accessTokenScopeRule(service.AccessTokenScopeOf(authz.ChannelSecretView)),

	"POST /api/channel/export": accessTokenSessionRule,

	// router/authz-router.go
	"GET /api/authz/catalog": accessTokenScopeRule("user:read"),

	// router/api-router.go: /api/token
	"GET /api/token/":            accessTokenScopeRule("api_key:read"),
	"GET /api/token/search":      accessTokenScopeRule("api_key:read"),
	"GET /api/token/auto-groups": accessTokenScopeRule("api_key:read"),
	"GET /api/token/:id":         accessTokenScopeRule("api_key:read"),
	"POST /api/token/:id/key":    accessTokenScopeRule("api_key:reveal"),
	"POST /api/token/":           accessTokenScopeRule("api_key:write"),
	"PUT /api/token/":            accessTokenScopeRule("api_key:write"),
	"DELETE /api/token/:id":      accessTokenScopeRule("api_key:write"),
	"POST /api/token/batch":      accessTokenScopeRule("api_key:write"),
	"POST /api/token/batch/keys": accessTokenScopeRule("api_key:reveal"),

	// router/api-router.go: /api/redemption
	"GET /api/redemption/":           accessTokenScopeRule(service.AccessTokenScopeOf(authz.RedemptionRead)),
	"GET /api/redemption/search":     accessTokenScopeRule(service.AccessTokenScopeOf(authz.RedemptionRead)),
	"GET /api/redemption/:id":        accessTokenScopeRule(service.AccessTokenScopeOf(authz.RedemptionRead)),
	"POST /api/redemption/":          accessTokenScopeRule(service.AccessTokenScopeOf(authz.RedemptionWrite)),
	"POST /api/redemption/batch":     accessTokenScopeRule(service.AccessTokenScopeOf(authz.RedemptionDelete)),
	"PUT /api/redemption/":           accessTokenScopeRule(service.AccessTokenScopeOf(authz.RedemptionWrite)),
	"DELETE /api/redemption/invalid": accessTokenScopeRule(service.AccessTokenScopeOf(authz.RedemptionDelete)),
	"DELETE /api/redemption/:id":     accessTokenScopeRule(service.AccessTokenScopeOf(authz.RedemptionDelete)),

	// router/api-router.go: /api/audit, /api/log, /api/data
	"GET /api/audit/self":                       accessTokenScopeRule("usage:read"),
	"GET /api/log/":                             accessTokenScopeRule(service.AccessTokenScopeOf(authz.LogRead)),
	"GET /api/log/stat":                         accessTokenScopeRule(service.AccessTokenScopeOf(authz.LogRead)),
	"GET /api/log/self/stat":                    accessTokenScopeRule("usage:read"),
	"GET /api/log/channel_affinity_usage_cache": accessTokenScopeRule(service.AccessTokenScopeOf(authz.LogRead)),
	"GET /api/log/search":                       accessTokenScopeRule(service.AccessTokenScopeOf(authz.LogRead)),
	"GET /api/log/self":                         accessTokenScopeRule("usage:read"),
	"GET /api/log/self/search":                  accessTokenScopeRule("usage:read"),
	"GET /api/log/export":                       accessTokenScopeRule("usage:read"),
	"GET /api/log/self/export":                  accessTokenScopeRule("usage:read"),
	"GET /api/data/":                            accessTokenScopeRule("log:read"),
	"GET /api/data/users":                       accessTokenScopeRule("log:read"),
	"GET /api/data/self":                        accessTokenScopeRule("usage:read"),
	"GET /api/data/flow":                        accessTokenScopeRule("log:read"),
	"GET /api/data/flow/self":                   accessTokenScopeRule("usage:read"),

	// router/api-router.go: /api/group, /api/prefill_group
	"GET /api/group/":               accessTokenScopeRule("group:read"),
	"GET /api/prefill_group/":       accessTokenScopeRule("group:read"),
	"POST /api/prefill_group/":      accessTokenScopeRule("group:write"),
	"PUT /api/prefill_group/":       accessTokenScopeRule("group:write"),
	"DELETE /api/prefill_group/:id": accessTokenScopeRule("group:write"),

	// router/api-router.go: /api/mj, /api/task
	"GET /api/mj/self":                 accessTokenScopeRule("usage:read"),
	"GET /api/mj/":                     accessTokenScopeRule("log:read"),
	"GET /api/task/self":               accessTokenScopeRule("usage:read"),
	"GET /api/task":                    accessTokenScopeRule("log:read"),
	"GET /api/task/:task_id/artifacts": accessTokenScopeRule("usage:read"),

	// router/api-router.go: /api/vendors, /api/models (admin)
	"POST /api/vendors/operations/preview":  accessTokenScopeRule(service.AccessTokenScopeOf(authz.ModelRead)),
	"POST /api/vendors/operations":          accessTokenScopeRule(service.AccessTokenScopeOf(authz.ModelWrite)),
	"GET /api/vendors/":                     accessTokenScopeRule(service.AccessTokenScopeOf(authz.ModelRead)),
	"GET /api/vendors/search":               accessTokenScopeRule(service.AccessTokenScopeOf(authz.ModelRead)),
	"GET /api/vendors/:id":                  accessTokenScopeRule(service.AccessTokenScopeOf(authz.ModelRead)),
	"POST /api/vendors/":                    accessTokenScopeRule(service.AccessTokenScopeOf(authz.ModelWrite)),
	"PUT /api/vendors/":                     accessTokenScopeRule(service.AccessTokenScopeOf(authz.ModelWrite)),
	"DELETE /api/vendors/:id":               accessTokenScopeRule(service.AccessTokenScopeOf(authz.ModelWrite)),
	"GET /api/models/sync_upstream/preview": accessTokenScopeRule(service.AccessTokenScopeOf(authz.ModelRead)),
	"POST /api/models/sync_upstream":        accessTokenScopeRule(service.AccessTokenScopeOf(authz.ModelWrite)),
	"POST /api/models/delete":               accessTokenScopeRule(service.AccessTokenScopeOf(authz.ModelWrite)),
	"GET /api/models/missing":               accessTokenScopeRule(service.AccessTokenScopeOf(authz.ModelRead)),
	"GET /api/models/":                      accessTokenScopeRule(service.AccessTokenScopeOf(authz.ModelRead)),
	"GET /api/models/search":                accessTokenScopeRule(service.AccessTokenScopeOf(authz.ModelRead)),
	"GET /api/models/:id":                   accessTokenScopeRule(service.AccessTokenScopeOf(authz.ModelRead)),
	"POST /api/models/":                     accessTokenScopeRule(service.AccessTokenScopeOf(authz.ModelWrite)),
	"PUT /api/models/":                      accessTokenScopeRule(service.AccessTokenScopeOf(authz.ModelWrite)),
	"DELETE /api/models/:id":                accessTokenScopeRule(service.AccessTokenScopeOf(authz.ModelWrite)),

	// router/api-router.go: /api/deployments

	"POST /api/user/purge_deleted": accessTokenScopeRule(service.AccessTokenScopeOf(authz.UserDelete)),

	"POST /api/user/batch/quota_action": accessTokenScopeRule(service.AccessTokenScopeOf(authz.UserManage)),

	"GET /api/log/export/raw": accessTokenScopeRule(service.AccessTokenScopeOf(authz.LogRead)),

	"GET /api/log/export-management": accessTokenScopeRule(service.AccessTokenScopeOf(authz.LogExportManage)),

	"POST /api/log/export-management/batch-delete": accessTokenScopeRule(service.AccessTokenScopeOf(authz.LogExportManage)),

	"POST /api/log/export-management/extend-expiration": accessTokenScopeRule(service.AccessTokenScopeOf(authz.LogExportManage)),

	"GET /api/log/export-management/:id/download": accessTokenScopeRule(service.AccessTokenScopeOf(authz.LogExportManage)),

	"POST /api/log/export-management/:id/share": accessTokenScopeRule(service.AccessTokenScopeOf(authz.LogExportManage)),

	"DELETE /api/log/export-management/:id/share": accessTokenScopeRule(service.AccessTokenScopeOf(authz.LogExportManage)),

	"DELETE /api/log/export-management/:id": accessTokenScopeRule(service.AccessTokenScopeOf(authz.LogExportManage)),

	"GET /api/option/ratio_schedules": accessTokenScopeRule("option:read"),

	"POST /api/option/ratio_schedules": accessTokenScopeRule("option:write"),

	"DELETE /api/option/ratio_schedules/:id": accessTokenScopeRule("option:write"),

	"POST /api/log/export": accessTokenScopeRule("usage:read"),

	"GET /api/log/export/:id/download": accessTokenScopeRule("usage:read"),

	"POST /api/log/export/:id/share": accessTokenScopeRule("usage:read"),

	"DELETE /api/log/export/:id/share": accessTokenScopeRule("usage:read"),

	"DELETE /api/log/export/:id": accessTokenScopeRule("usage:read"),

	// router/relay-router.go
	"POST /pg/chat/completions": accessTokenSessionRule,
}

var (
	permissionRouteRulesMu sync.RWMutex
	permissionRouteRules   = map[string]accessTokenRouteRule{}
)

// DeclareAccessTokenPermissionRoute records that the Casbin permission guarding
// a route is also the token scope it requires. It runs while routes are
// registered and panics on conflicting declarations.
func DeclareAccessTokenPermissionRoute(method, fullPath string, permission authz.Permission) {
	key := method + " " + fullPath
	rule := accessTokenScopeRule(service.AccessTokenScopeOf(permission))
	if _, ok := accessTokenRouteRules[key]; ok {
		panic(fmt.Sprintf("access token route %s is declared twice", key))
	}
	permissionRouteRulesMu.Lock()
	defer permissionRouteRulesMu.Unlock()
	if existing, ok := permissionRouteRules[key]; ok && existing != rule {
		panic(fmt.Sprintf("access token route %s is declared with %s and %s", key, existing.scope, rule.scope))
	}
	permissionRouteRules[key] = rule
}

// AccessTokenRouteRule returns the rule for "METHOD /full/path".
func AccessTokenRouteRule(key string) (accessTokenRouteRule, bool) {
	if rule, ok := accessTokenRouteRules[key]; ok {
		return rule, true
	}
	permissionRouteRulesMu.RLock()
	defer permissionRouteRulesMu.RUnlock()
	rule, ok := permissionRouteRules[key]
	return rule, ok
}

// AccessTokenRouteRuleKeys lists every declared route key, sorted.
func AccessTokenRouteRuleKeys() []string {
	permissionRouteRulesMu.RLock()
	keys := slices.Collect(maps.Keys(permissionRouteRules))
	permissionRouteRulesMu.RUnlock()
	keys = slices.AppendSeq(keys, maps.Keys(accessTokenRouteRules))
	slices.Sort(keys)
	return keys
}

// Scope returns the token scope the rule requires, or "" for any/session rules.
func (rule accessTokenRouteRule) Scope() string {
	return rule.scope
}

// Kind names the rule category: "scope", "any" or "session".
func (rule accessTokenRouteRule) Kind() string {
	switch rule.kind {
	case accessTokenRuleScope:
		return "scope"
	case accessTokenRuleAny:
		return "any"
	case accessTokenRuleSession:
		return "session"
	}
	return ""
}
