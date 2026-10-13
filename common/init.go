package common

import (
	"flag"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"new-router/constant"
)

var (
	Port         = flag.Int("port", 3000, "the listening port")
	PrintVersion = flag.Bool("version", false, "print version and exit")
	PrintHelp    = flag.Bool("help", false, "print help and exit")
	LogDir       = flag.String("log-dir", "./logs", "specify the log directory")
)

func printHelp() {
	fmt.Println("New Router (Based on One API) " + Version + " - The next-generation LLM gateway and AI asset management system supports multiple languages.")
	fmt.Println("Original Project: OneAPI by JustSong - https://github.com/songquanpeng/one-api")
	fmt.Println("Project: New Router - https://github.com/Rfym21/WorkSpace/tree/main/Projects/NewRouter")
	fmt.Println("Upstream Project: New API by QuantumNous - https://github.com/QuantumNous/new-api")
	fmt.Println("Usage: new-router [--port <port>] [--log-dir <log directory>] [--version] [--help]")
}

func InitEnv() {
	flag.Parse()

	envVersion := os.Getenv("VERSION")
	if envVersion != "" {
		Version = envVersion
	}

	if *PrintVersion {
		fmt.Println(Version)
		os.Exit(0)
	}

	if *PrintHelp {
		printHelp()
		os.Exit(0)
	}

	DefaultLanguage = os.Getenv("DEFAULT_LANGUAGE")
	SetLogLanguage(DefaultLanguage)

	if os.Getenv("SESSION_SECRET") != "" {
		ss := os.Getenv("SESSION_SECRET")
		if ss == "random_string" {
			log.Println(LogText("WARNING: SESSION_SECRET is set to the default value 'random_string', please change it to a random string."))
			log.Fatal(LogText("Please set SESSION_SECRET to a random string."))
		} else {
			SessionSecret = ss
		}
	}
	if os.Getenv("CRYPTO_SECRET") != "" {
		CryptoSecret = os.Getenv("CRYPTO_SECRET")
	} else {
		CryptoSecret = SessionSecret
	}
	if err := InitSessionCookieSettings(); err != nil {
		log.Fatal(err)
	}
	initUserSessionSettings()
	if os.Getenv("SQLITE_PATH") != "" {
		SQLitePath = os.Getenv("SQLITE_PATH")
	}
	if *LogDir != "" {
		var err error
		*LogDir, err = filepath.Abs(*LogDir)
		if err != nil {
			log.Fatal(err)
		}
		if _, err := os.Stat(*LogDir); os.IsNotExist(err) {
			err = os.Mkdir(*LogDir, 0777)
			if err != nil {
				log.Fatal(err)
			}
		}
	}

	// Initialize variables from constants.go that were using environment variables
	MemoryCacheEnabled = os.Getenv("MEMORY_CACHE_ENABLED") == "true"
	IsMasterNode = os.Getenv("NODE_TYPE") != "slave"
	PasswordLoginEncryptionEnabled = GetEnvOrDefaultBool("PASSWORD_LOGIN_ENCRYPTION_ENABLED", false)
	initNodeNameIdentity()
	TLSInsecureSkipVerify = GetEnvOrDefaultBool("TLS_INSECURE_SKIP_VERIFY", false)
	if TLSInsecureSkipVerify {
		if tr, ok := http.DefaultTransport.(*http.Transport); ok && tr != nil {
			if tr.TLSClientConfig != nil {
				tr.TLSClientConfig.InsecureSkipVerify = true
			} else {
				tr.TLSClientConfig = InsecureTLSConfig.Clone()
			}
		}
	}

	// 中继超时/连接池、全局限流、POLLING_INTERVAL、调试与统计开关等原环境变量配置
	// 已迁移为 runtime_setting 热更新选项,由 runtime_setting.ApplyToRuntime 写入对应全局量。
	SyncFrequency = GetEnvOrDefault("SYNC_FREQUENCY", 60)
	BatchUpdateInterval = GetEnvOrDefault("BATCH_UPDATE_INTERVAL", 5)
	RelayIdleConnTimeout = GetEnvOrDefault("RELAY_IDLE_CONN_TIMEOUT", 90)
	RelayResponseHeaderTimeout = GetEnvOrDefault("RELAY_RESPONSE_HEADER_TIMEOUT", 1800)

	// Initialize string variables with GetEnvOrDefaultString
	CohereSafetySetting = GetEnvOrDefaultString("COHERE_SAFETY_SETTING", "NONE")
	GlobalStaticRateLimitEnable = GetEnvOrDefaultBool("GLOBAL_STATIC_RATE_LIMIT_ENABLE", false)
	GlobalStaticRateLimitNum = GetEnvOrDefault("GLOBAL_STATIC_RATE_LIMIT", 1000)
	GlobalStaticRateLimitDuration = int64(GetEnvOrDefault("GLOBAL_STATIC_RATE_LIMIT_DURATION", 180))
	initConstantEnv()
}

func initUserSessionSettings() {
	UserSessionActiveLimit = positiveUserSessionEnv("USER_SESSION_ACTIVE_LIMIT", DefaultUserSessionActiveLimit)
	UserSessionIssuanceLimit = positiveUserSessionEnv("USER_SESSION_ISSUANCE_LIMIT", DefaultUserSessionIssuanceLimit)
	UserSessionIssuanceWindowSeconds = int64(positiveUserSessionEnv("USER_SESSION_ISSUANCE_WINDOW_SECONDS", DefaultUserSessionIssuanceWindowSeconds))
	UserSessionRevokedRetentionDays = positiveUserSessionEnv("USER_SESSION_REVOKED_RETENTION_DAYS", DefaultUserSessionRevokedRetentionDays)
	UserSessionHourlyAlertThreshold = positiveUserSessionEnv("USER_SESSION_HOURLY_ALERT_THRESHOLD", DefaultUserSessionHourlyAlertThreshold)

	const secondsPerDay = 24 * 60 * 60
	if int64(UserSessionRevokedRetentionDays) > math.MaxInt64/secondsPerDay {
		SysError(LogText(
			"USER_SESSION_REVOKED_RETENTION_DAYS is too large, using default value: %d",
			DefaultUserSessionRevokedRetentionDays,
		))
		UserSessionRevokedRetentionDays = DefaultUserSessionRevokedRetentionDays
	}
	retentionSeconds := int64(UserSessionRevokedRetentionDays) * secondsPerDay
	if UserSessionIssuanceWindowSeconds > retentionSeconds {
		configuredWindow := UserSessionIssuanceWindowSeconds
		UserSessionIssuanceWindowSeconds = retentionSeconds
		SysError(LogText(
			"USER_SESSION_ISSUANCE_WINDOW_SECONDS exceeds revoked retention; configured_window_seconds=%d revoked_retention_seconds=%d effective_window_seconds=%d",
			configuredWindow,
			retentionSeconds,
			UserSessionIssuanceWindowSeconds,
		))
	}
}

func positiveUserSessionEnv(name string, fallback int) int {
	value := GetEnvOrDefault(name, fallback)
	if value <= 0 {
		SysError(LogText("%s must be positive, using default value: %d", name, fallback))
		return fallback
	}
	return value
}

func initConstantEnv() {
	constant.DifyDebug = GetEnvOrDefaultBool("DIFY_DEBUG", true)
	constant.AnonymousRequestBodyLimitKB = GetEnvOrDefault("ANONYMOUS_REQUEST_BODY_LIMIT_KB", 512)
	constant.UpdateTask = GetEnvOrDefaultBool("UPDATE_TASK", true)
	constant.TaskPluginEnabled = GetEnvOrDefaultBool("TASK_PLUGIN_ENABLED", true)
	// GenerateDefaultToken 是否生成初始令牌，默认关闭。
	constant.GenerateDefaultToken = GetEnvOrDefaultBool("GENERATE_DEFAULT_TOKEN", false)
	// 任务轮询时查询的最大数量
	constant.TaskQueryLimit = GetEnvOrDefault("TASK_QUERY_LIMIT", 1000)
	// 异步任务超时时间（分钟），超过此时间未完成的任务将被标记为失败并退款。0 表示禁用。
	constant.TaskTimeoutMinutes = GetEnvOrDefault("TASK_TIMEOUT_MINUTES", 1440)
	// Consecutive unrecognized/transient poll failures before the task is failed and refunded.
	constant.TaskPollMaxFailures = GetEnvOrDefault("TASK_POLL_MAX_FAILURES", 20)
	// 声明式任务协议桥只观察数据库；这些值控制一次客户端观察连接，
	// 不改变后台轮询或结算生命周期。
	constant.TaskPluginProtocolTimeoutSeconds = GetEnvOrDefault("TASK_PLUGIN_PROTOCOL_TIMEOUT_SECONDS", 600)
	constant.TaskPluginProtocolTickMilliseconds = GetEnvOrDefault("TASK_PLUGIN_PROTOCOL_TICK_MILLISECONDS", 2000)
	constant.TaskPluginProtocolTickJitterMilliseconds = GetEnvOrDefault("TASK_PLUGIN_PROTOCOL_TICK_JITTER_MILLISECONDS", 500)
	constant.TaskPluginProtocolHeartbeatSeconds = GetEnvOrDefault("TASK_PLUGIN_PROTOCOL_HEARTBEAT_SECONDS", 15)

	soraPatchStr := GetEnvOrDefaultString("TASK_PRICE_PATCH", "")
	if soraPatchStr != "" {
		var taskPricePatches []string
		soraPatches := strings.SplitSeq(soraPatchStr, ",")
		for patch := range soraPatches {
			trimmedPatch := strings.TrimSpace(patch)
			if trimmedPatch != "" {
				taskPricePatches = append(taskPricePatches, trimmedPatch)
			}
		}
		constant.TaskPricePatches = taskPricePatches
	}

	// Initialize trusted redirect domains for URL validation
	trustedDomainsStr := GetEnvOrDefaultString("TRUSTED_REDIRECT_DOMAINS", "")
	var trustedDomains []string
	domains := strings.SplitSeq(trustedDomainsStr, ",")
	for domain := range domains {
		trimmedDomain := strings.TrimSpace(domain)
		if trimmedDomain != "" {
			// Normalize domain to lowercase
			trustedDomains = append(trustedDomains, strings.ToLower(trimmedDomain))
		}
	}
	constant.TrustedRedirectDomains = trustedDomains
}
