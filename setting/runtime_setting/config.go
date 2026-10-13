package runtime_setting

import (
	"strings"
	"time"

	"new-router/common"
	"new-router/constant"
	"new-router/setting/config"
)

// RuntimeSetting 承载可在管理端热更新的运行时配置,取代原环境变量。
// 默认值即代码基线;持久化值经 option 表加载并通过 handleConfigUpdate 热应用。
type RuntimeSetting struct {
	RelayTimeout                  int    `json:"relay_timeout"`
	StreamingTimeout              int    `json:"streaming_timeout"`
	RelayMaxIdleConns             int    `json:"relay_max_idle_conns"`
	RelayMaxIdleConnsPerHost      int    `json:"relay_max_idle_conns_per_host"`
	MaxRequestBodyMB              int    `json:"max_request_body_mb"`
	StreamScannerMaxBufferMB      int    `json:"stream_scanner_max_buffer_mb"`
	GlobalAPIRateLimitEnabled     bool   `json:"global_api_rate_limit_enabled"`
	GlobalAPIRateLimit            int    `json:"global_api_rate_limit"`
	GlobalAPIRateLimitDuration    int    `json:"global_api_rate_limit_duration"`
	GlobalWebRateLimitEnabled     bool   `json:"global_web_rate_limit_enabled"`
	GlobalWebRateLimit            int    `json:"global_web_rate_limit"`
	GlobalWebRateLimitDuration    int    `json:"global_web_rate_limit_duration"`
	CriticalRateLimitEnabled      bool   `json:"critical_rate_limit_enabled"`
	CriticalRateLimit             int    `json:"critical_rate_limit"`
	CriticalRateLimitDuration     int    `json:"critical_rate_limit_duration"`
	SearchRateLimitEnabled        bool   `json:"search_rate_limit_enabled"`
	SearchRateLimit               int    `json:"search_rate_limit"`
	SearchRateLimitDuration       int    `json:"search_rate_limit_duration"`
	AzureDefaultAPIVersion        string `json:"azure_default_api_version"`
	ForceStreamOptionEnabled      bool   `json:"force_stream_option_enabled"`
	CountTokenEnabled             bool   `json:"count_token_enabled"`
	GetMediaTokenEnabled          bool   `json:"get_media_token_enabled"`
	GetMediaTokenNotStreamEnabled bool   `json:"get_media_token_not_stream_enabled"`
	MaxFileDownloadMB             int    `json:"max_file_download_mb"`
	ChannelTestRequestInterval    int    `json:"channel_test_request_interval"`
	NotifyLimitCount              int    `json:"notify_limit_count"`
	NotificationLimitDuration     int    `json:"notification_limit_duration"`
	DebugEnabled                  bool   `json:"debug_enabled"`
	AccessLogEnabled              bool   `json:"access_log_enabled"`
	ErrorLogEnabled               bool   `json:"error_log_enabled"`
	SyncUpstreamBase              string `json:"sync_upstream_base"`
	SyncHTTPTimeoutSeconds        int    `json:"sync_http_timeout_seconds"`
	SyncHTTPRetry                 int    `json:"sync_http_retry"`
	SyncHTTPMaxMB                 int    `json:"sync_http_max_mb"`
	LinuxDOTokenEndpoint          string `json:"linux_do_token_endpoint"`
	LinuxDOUserEndpoint           string `json:"linux_do_user_endpoint"`
}

// 默认值保持迁移前的项目行为基线(限流 360/120、访问日志开、流式缓冲 128MB 无上限)。
var runtimeSetting = RuntimeSetting{
	RelayTimeout:                  0,
	StreamingTimeout:              300,
	RelayMaxIdleConns:             500,
	RelayMaxIdleConnsPerHost:      100,
	MaxRequestBodyMB:              128,
	StreamScannerMaxBufferMB:      128,
	GlobalAPIRateLimitEnabled:     true,
	GlobalAPIRateLimit:            360,
	GlobalAPIRateLimitDuration:    180,
	GlobalWebRateLimitEnabled:     true,
	GlobalWebRateLimit:            120,
	GlobalWebRateLimitDuration:    180,
	CriticalRateLimitEnabled:      true,
	CriticalRateLimit:             20,
	CriticalRateLimitDuration:     20 * 60,
	SearchRateLimitEnabled:        true,
	SearchRateLimit:               10,
	SearchRateLimitDuration:       60,
	AzureDefaultAPIVersion:        "2025-04-01-preview",
	ForceStreamOptionEnabled:      true,
	CountTokenEnabled:             true,
	GetMediaTokenEnabled:          true,
	GetMediaTokenNotStreamEnabled: false,
	MaxFileDownloadMB:             64,
	ChannelTestRequestInterval:    0,
	NotifyLimitCount:              2,
	NotificationLimitDuration:     10,
	DebugEnabled:                  false,
	AccessLogEnabled:              true,
	ErrorLogEnabled:               false,
	SyncUpstreamBase:              "https://basellm.github.io/llm-metadata",
	SyncHTTPTimeoutSeconds:        15,
	SyncHTTPRetry:                 3,
	SyncHTTPMaxMB:                 10,
	LinuxDOTokenEndpoint:          "https://connect.linux.do/oauth2/token",
	LinuxDOUserEndpoint:           "https://connect.linux.do/api/user",
}

func init() {
	config.GlobalConfig.Register("runtime_setting", &runtimeSetting)
}

// transportConfig 记录已应用的 HTTP 传输层参数,用于检测是否需要重建连接池。
type transportConfig struct {
	relayTimeout        int
	maxIdleConns        int
	maxIdleConnsPerHost int
}

var (
	transportReloadHooks []func()
	appliedTransport     transportConfig
)

// RegisterTransportReloadHook 注册 HTTP 传输层参数变更时的重建回调(如 InitHttpClient)。
// 仅应在启动阶段(SyncOptions 协程启动前)调用,不做并发保护。
func RegisterTransportReloadHook(hook func()) {
	transportReloadHooks = append(transportReloadHooks, hook)
}

// ApplyToRuntime 把可热更新配置镜像到请求路径实际读取的 common.*/constant.* 全局量。
// 周期性 SyncOptions 会按 key 重复触发,故传输层重建仅在参数实际变化时执行。
func ApplyToRuntime() {
	common.RelayTimeout = runtimeSetting.RelayTimeout
	common.RelayMaxIdleConns = runtimeSetting.RelayMaxIdleConns
	common.RelayMaxIdleConnsPerHost = runtimeSetting.RelayMaxIdleConnsPerHost
	common.GlobalApiRateLimitEnable = runtimeSetting.GlobalAPIRateLimitEnabled
	common.GlobalApiRateLimitNum = runtimeSetting.GlobalAPIRateLimit
	common.GlobalApiRateLimitDuration = int64(runtimeSetting.GlobalAPIRateLimitDuration)
	common.GlobalWebRateLimitEnable = runtimeSetting.GlobalWebRateLimitEnabled
	common.GlobalWebRateLimitNum = runtimeSetting.GlobalWebRateLimit
	common.GlobalWebRateLimitDuration = int64(runtimeSetting.GlobalWebRateLimitDuration)
	common.CriticalRateLimitEnable = runtimeSetting.CriticalRateLimitEnabled
	common.CriticalRateLimitNum = runtimeSetting.CriticalRateLimit
	common.CriticalRateLimitDuration = int64(runtimeSetting.CriticalRateLimitDuration)
	common.SearchRateLimitEnable = runtimeSetting.SearchRateLimitEnabled
	common.SearchRateLimitNum = runtimeSetting.SearchRateLimit
	common.SearchRateLimitDuration = int64(runtimeSetting.SearchRateLimitDuration)
	common.RequestInterval = time.Duration(runtimeSetting.ChannelTestRequestInterval) * time.Second
	common.DebugEnabled = runtimeSetting.DebugEnabled
	common.AccessLogEnabled = runtimeSetting.AccessLogEnabled
	if runtimeSetting.StreamingTimeout < 1 {
		constant.StreamingTimeout = 300
	} else {
		constant.StreamingTimeout = runtimeSetting.StreamingTimeout
	}
	constant.MaxFileDownloadMB = runtimeSetting.MaxFileDownloadMB
	constant.StreamScannerMaxBufferMB = runtimeSetting.StreamScannerMaxBufferMB
	constant.MaxRequestBodyMB = runtimeSetting.MaxRequestBodyMB
	constant.ForceStreamOption = runtimeSetting.ForceStreamOptionEnabled
	constant.CountToken = runtimeSetting.CountTokenEnabled
	constant.GetMediaToken = runtimeSetting.GetMediaTokenEnabled
	constant.GetMediaTokenNotStream = runtimeSetting.GetMediaTokenNotStreamEnabled
	// 空值回落默认版本:清空面板输入不得让 Azure 渠道构造出 "?api-version=" 的坏请求。
	if strings.TrimSpace(runtimeSetting.AzureDefaultAPIVersion) == "" {
		constant.AzureDefaultAPIVersion = "2025-04-01-preview"
	} else {
		constant.AzureDefaultAPIVersion = runtimeSetting.AzureDefaultAPIVersion
	}
	constant.NotifyLimitCount = runtimeSetting.NotifyLimitCount
	constant.NotificationLimitDurationMinute = runtimeSetting.NotificationLimitDuration
	constant.ErrorLogEnabled = runtimeSetting.ErrorLogEnabled

	current := transportConfig{
		relayTimeout:        runtimeSetting.RelayTimeout,
		maxIdleConns:        runtimeSetting.RelayMaxIdleConns,
		maxIdleConnsPerHost: runtimeSetting.RelayMaxIdleConnsPerHost,
	}
	if current != appliedTransport {
		appliedTransport = current
		for _, hook := range transportReloadHooks {
			hook()
		}
	}
}

// GetSyncUpstreamBase 返回模型元数据同步上游基址。
func GetSyncUpstreamBase() string {
	if runtimeSetting.SyncUpstreamBase == "" {
		return "https://basellm.github.io/llm-metadata"
	}
	return runtimeSetting.SyncUpstreamBase
}

// GetSyncHTTPTimeoutSeconds 返回模型同步 HTTP 超时秒数。
func GetSyncHTTPTimeoutSeconds() int {
	if runtimeSetting.SyncHTTPTimeoutSeconds < 1 {
		return 15
	}
	return runtimeSetting.SyncHTTPTimeoutSeconds
}

// GetSyncHTTPRetry 返回模型同步 HTTP 重试次数。
func GetSyncHTTPRetry() int {
	if runtimeSetting.SyncHTTPRetry < 1 {
		return 1
	}
	return runtimeSetting.SyncHTTPRetry
}

// GetSyncHTTPMaxMB 返回模型同步 HTTP 响应大小上限(MB)。
func GetSyncHTTPMaxMB() int {
	if runtimeSetting.SyncHTTPMaxMB < 1 {
		return 1
	}
	return runtimeSetting.SyncHTTPMaxMB
}

// GetLinuxDOTokenEndpoint 返回 Linux DO OAuth token 端点。
func GetLinuxDOTokenEndpoint() string {
	if runtimeSetting.LinuxDOTokenEndpoint == "" {
		return "https://connect.linux.do/oauth2/token"
	}
	return runtimeSetting.LinuxDOTokenEndpoint
}

// GetLinuxDOUserEndpoint 返回 Linux DO OAuth 用户信息端点。
func GetLinuxDOUserEndpoint() string {
	if runtimeSetting.LinuxDOUserEndpoint == "" {
		return "https://connect.linux.do/api/user"
	}
	return runtimeSetting.LinuxDOUserEndpoint
}
