package constant

type ContextKey string

const (
	ContextKeyTokenCountMeta  ContextKey = "token_count_meta"
	ContextKeyPromptTokens    ContextKey = "prompt_tokens"
	ContextKeyEstimatedTokens ContextKey = "estimated_tokens"

	ContextKeyOriginalModel    ContextKey = "original_model"
	ContextKeyRequestStartTime ContextKey = "request_start_time"

	/* token related keys */
	ContextKeyTokenUnlimited         ContextKey = "token_unlimited_quota"
	ContextKeyTokenKey               ContextKey = "token_key"
	ContextKeyTokenId                ContextKey = "token_id"
	ContextKeyTokenGroup             ContextKey = "token_group"
	ContextKeyOriginTasks            ContextKey = "origin_tasks"
	ContextKeyChannelConstraints     ContextKey = "channel_constraints"
	ContextKeyTokenModelLimitEnabled ContextKey = "token_model_limit_enabled"
	ContextKeyTokenModelLimit        ContextKey = "token_model_limit"
	ContextKeyTokenCrossGroupRetry   ContextKey = "token_cross_group_retry"
	ContextKeyTokenAutoGroups        ContextKey = "token_auto_groups"

	/* channel related keys */
	ContextKeyChannelId                ContextKey = "channel_id"
	ContextKeyChannelName              ContextKey = "channel_name"
	ContextKeyChannelCreateTime        ContextKey = "channel_create_time"
	ContextKeyChannelBaseUrl           ContextKey = "base_url"
	ContextKeyChannelType              ContextKey = "channel_type"
	ContextKeyChannelSetting           ContextKey = "channel_setting"
	ContextKeyChannelOtherSetting      ContextKey = "channel_other_setting"
	ContextKeyChannelParamOverride     ContextKey = "param_override"
	ContextKeyChannelHeaderOverride    ContextKey = "header_override"
	ContextKeyChannelOrganization      ContextKey = "channel_organization"
	ContextKeyChannelAutoBan           ContextKey = "auto_ban"
	ContextKeyChannelModelMapping      ContextKey = "model_mapping"
	ContextKeyChannelStatusCodeMapping ContextKey = "status_code_mapping"
	ContextKeyChannelIsMultiKey        ContextKey = "channel_is_multi_key"
	ContextKeyChannelMultiKeyIndex     ContextKey = "channel_multi_key_index"
	ContextKeyChannelKey               ContextKey = "channel_key"

	ContextKeyAutoGroup           ContextKey = "auto_group"
	ContextKeyAutoGroupIndex      ContextKey = "auto_group_index"
	ContextKeyAutoGroupRetryIndex ContextKey = "auto_group_retry_index"

	/* user related keys */
	ContextKeyUserId      ContextKey = "id"
	ContextKeyUserSetting ContextKey = "user_setting"
	ContextKeyUserQuota   ContextKey = "user_quota"
	ContextKeyUserStatus  ContextKey = "user_status"
	ContextKeyUserEmail   ContextKey = "user_email"
	ContextKeyUserGroup   ContextKey = "user_group"
	ContextKeyUsingGroup  ContextKey = "group"
	ContextKeyUserName    ContextKey = "username"

	ContextKeyLocalCountTokens ContextKey = "local_count_tokens"

	ContextKeySystemPromptOverride ContextKey = "system_prompt_override"

	// ContextKeyFileSourcesToCleanup stores file sources that need cleanup when request ends
	ContextKeyFileSourcesToCleanup ContextKey = "file_sources_to_cleanup"

	// ContextKeyAdminRejectReason stores an admin-only reject/block reason extracted from upstream responses.
	// It is not returned to end users, but can be persisted into consume/error logs for debugging.
	ContextKeyAdminRejectReason ContextKey = "admin_reject_reason"

	// ContextKeyLanguage stores the user's language preference for i18n
	ContextKeyLanguage             ContextKey = "language"
	ContextKeyIsStream             ContextKey = "is_stream"
	ContextKeyResponseStreamStatus ContextKey = "response_stream_status"

	// ContextKeyAuditLogged marks that the current request has already recorded
	// a manage/operation audit log inside the handler. When set, the admin-audit
	// fallback in authHelper (finishAdminAudit) skips its record to avoid
	// duplicate entries.
	ContextKeyAuditLogged ContextKey = "audit_logged"

	// ContextKeyClaudeForceCacheInjected 标记本次请求是否由「渠道强制缓存」实际注入了 cache_control。
	// 仅当注入发生（即原请求未携带任何 cache_control 标记）时为 true；
	// 响应阶段据此决定是否重写 usage（用户自带 cache_control 时完全透传 usage）。
	ContextKeyClaudeForceCacheInjected ContextKey = "claude_force_cache_injected"
	// ContextKeyClaudeCache1hAs5m 标记本次请求是否启用「1h 缓存按 5m 计费」改写。
	// 响应阶段据此把 cache_creation.ephemeral_1h_input_tokens 合并到 5m 字段。
	ContextKeyClaudeCache1hAs5m ContextKey = "claude_cache_1h_as_5m"
	// ContextKeyClaudeCacheReadMinRatio 存储本次请求的缓存读最低占比阈值（float64，0-1）。
	// 仅当渠道开启「缓存读写比例控制器」且阈值 > 0 时设置；响应阶段据此重写 cache_read/cache_creation_input_tokens 比例。
	ContextKeyClaudeCacheReadMinRatio ContextKey = "claude_cache_read_min_ratio"

	// ContextKeyTokenAuditParams contains only the API token operation's safe metadata.
	ContextKeyTokenAuditParams ContextKey = "token_audit_params"
	// ContextKeyTokenAuditSucceeded disambiguates token responses that exceed the audit buffer.
	ContextKeyTokenAuditSucceeded ContextKey = "token_audit_succeeded"
)
