package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
	"new-router/common"
	"new-router/logger"
)

const (
	redisRateLimitTimeout             = 2 * time.Second
	redisRateLimitNamespace           = "rateLimit:v2"
	redisSlidingWindowRateLimitScript = `
local key = KEYS[1]
local server_time = redis.call("TIME")
local now = tonumber(server_time[1]) * 1000 + math.floor(tonumber(server_time[2]) / 1000)
local window = tonumber(ARGV[1])
local max_count = tonumber(ARGV[2])
local ttl = tonumber(ARGV[3])
local member = ARGV[4]

local key_type_reply = redis.call("TYPE", key)
local key_type = key_type_reply
if type(key_type_reply) == "table" then
  key_type = key_type_reply.ok
end
if key_type ~= "none" and key_type ~= "zset" then
  redis.call("DEL", key)
end

redis.call("ZREMRANGEBYSCORE", key, "-inf", now - window)
local count = redis.call("ZCARD", key)
if count >= max_count then
  redis.call("EXPIRE", key, ttl)
  return 0
end

redis.call("ZADD", key, now, member)
redis.call("EXPIRE", key, ttl)
return 1
`
	// Redis IP and user limiters intentionally retain upstream's fixed-window
	// behavior. Model success accounting uses the separate sliding-window script.
	redisFixedWindowScript = `
local count = redis.call('INCR', KEYS[1])
if count == 1 then
  redis.call('EXPIRE', KEYS[1], ARGV[2])
end
local ttl = redis.call('TTL', KEYS[1])
if ttl < 0 then
  redis.call('EXPIRE', KEYS[1], ARGV[2])
  ttl = redis.call('TTL', KEYS[1])
end
if count > tonumber(ARGV[1]) then
  return {0, count, ttl}
end
return {1, count, ttl}
`
)

var inMemoryRateLimiter common.InMemoryRateLimiter
var redisSlidingWindowScript redisRateLimitScriptCache
var redisRateLimitMemberPrefix = common.GetUUID()
var redisRateLimitMemberSequence atomic.Uint64

type redisRateLimitScriptCache struct {
	mu  sync.RWMutex
	sha string
}

func redisIPRateLimitKey(mark string, clientIP string) string {
	return fmt.Sprintf("%s:ip:%s:%s", redisRateLimitNamespace, mark, clientIP)
}

func redisUserRateLimitKey(mark string, userID int) string {
	return fmt.Sprintf("%s:user:%s:%d", redisRateLimitNamespace, mark, userID)
}

func redisReplyInteger(value any) (int64, error) {
	switch typed := value.(type) {
	case int64:
		return typed, nil
	case string:
		return strconv.ParseInt(typed, 10, 64)
	case []byte:
		return strconv.ParseInt(string(typed), 10, 64)
	default:
		return 0, fmt.Errorf("unexpected Redis integer reply type %T", value)
	}
}

func redisFixedWindowTake(ctx context.Context, key string, maxRequestNum int, duration int64) (bool, int64, int64, error) {
	if common.RDB == nil {
		return false, 0, 0, errors.New("Redis client is not initialized")
	}
	if key == "" {
		return false, 0, 0, errors.New("rate limit key is empty")
	}
	if maxRequestNum <= 0 {
		return false, 0, 0, errors.New("rate limit maximum must be positive")
	}
	if duration <= 0 {
		return false, 0, 0, errors.New("rate limit duration must be positive")
	}

	values, err := common.RDB.Eval(
		ctx,
		redisFixedWindowScript,
		[]string{key},
		maxRequestNum,
		duration,
	).Slice()
	if err != nil {
		return false, 0, 0, err
	}
	if len(values) != 3 {
		return false, 0, 0, fmt.Errorf("unexpected Redis rate limit reply length %d", len(values))
	}

	allowedValue, err := redisReplyInteger(values[0])
	if err != nil {
		return false, 0, 0, err
	}
	count, err := redisReplyInteger(values[1])
	if err != nil {
		return false, 0, 0, err
	}
	ttlSeconds, err := redisReplyInteger(values[2])
	if err != nil {
		return false, 0, 0, err
	}

	return allowedValue == 1, count, ttlSeconds, nil
}

func redisRateLimiter(c *gin.Context, maxRequestNum int, duration int64, mark string) {
	allowed, _, ttlSeconds, err := redisFixedWindowTake(
		c.Request.Context(),
		redisIPRateLimitKey(mark, c.ClientIP()),
		maxRequestNum,
		duration,
	)
	if err != nil {
		logger.LogError(c.Request.Context(), common.LogText("rate limit check failed (mark=%s): %v", mark, err))
		c.Status(http.StatusInternalServerError)
		c.Abort()
		return
	}
	if !allowed {
		writeRateLimited(c, ttlSeconds)
	}
}

func redisRateLimitContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), redisRateLimitTimeout)
}

func redisSlidingWindowReserve(ctx context.Context, key string, maxRequestNum int, duration int64, expiration time.Duration) (bool, string, error) {
	if maxRequestNum <= 0 {
		return true, "", nil
	}

	member := fmt.Sprintf(
		"%s:%d",
		redisRateLimitMemberPrefix,
		redisRateLimitMemberSequence.Add(1),
	)
	ttlSeconds := int64(expiration / time.Second)
	if ttlSeconds <= 0 {
		ttlSeconds = duration
	}
	if ttlSeconds <= 0 {
		ttlSeconds = 1
	}
	windowMillis := duration * 1000
	if windowMillis <= 0 {
		windowMillis = 1
	}

	result, err := redisSlidingWindowScript.eval(ctx, key, windowMillis, maxRequestNum, ttlSeconds, member)
	if err != nil {
		return false, "", err
	}
	return result == 1, member, nil
}

func (s *redisRateLimitScriptCache) eval(ctx context.Context, key string, args ...any) (int, error) {
	if sha := s.get(); sha != "" {
		result, err := common.RDB.EvalSha(ctx, sha, []string{key}, args...).Int()
		if err == nil {
			return result, nil
		}
		if !isRedisNoScriptError(err) {
			return 0, err
		}
	}

	if err := s.load(ctx); err == nil {
		if sha := s.get(); sha != "" {
			result, err := common.RDB.EvalSha(ctx, sha, []string{key}, args...).Int()
			if err == nil {
				return result, nil
			}
			if !isRedisNoScriptError(err) {
				return 0, err
			}
		}
	}

	return common.RDB.Eval(ctx, redisSlidingWindowRateLimitScript, []string{key}, args...).Int()
}

func (s *redisRateLimitScriptCache) get() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sha
}

func (s *redisRateLimitScriptCache) load(ctx context.Context) error {
	sha, err := common.RDB.ScriptLoad(ctx, redisSlidingWindowRateLimitScript).Result()
	if err != nil {
		return fmt.Errorf("load redis rate limit script: %w", err)
	}
	s.mu.Lock()
	s.sha = sha
	s.mu.Unlock()
	return nil
}

func isRedisNoScriptError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToUpper(err.Error())
	return strings.Contains(message, "NOSCRIPT") || strings.Contains(message, "NO MATCHING SCRIPT")
}

func releaseRedisSlidingWindow(key string, member string) {
	if member == "" {
		return
	}
	ctx, cancel := redisRateLimitContext()
	defer cancel()
	if err := common.RDB.ZRem(ctx, key, member).Err(); err != nil {
		common.SysError("release redis rate limit reservation failed: " + err.Error())
	}
}

func memoryRateLimiter(c *gin.Context, maxRequestNum int, duration int64, mark string) {
	key := mark + c.ClientIP()
	if !inMemoryRateLimiter.Request(key, maxRequestNum, duration) {
		writeRateLimited(c, duration)
		return
	}
}

// writeRateLimited rejects the request with 429 and a Retry-After hint so
// clients can back off instead of treating the rejection as a fatal error.
// The in-memory limiter cannot report the remaining window, so callers
// without a TTL pass the full window duration as a conservative upper bound.
func writeRateLimited(c *gin.Context, retryAfterSeconds int64) {
	if retryAfterSeconds > 0 {
		c.Header("Retry-After", strconv.FormatInt(retryAfterSeconds, 10))
	}
	c.Status(http.StatusTooManyRequests)
	c.Abort()
}

func rateLimitFactory(maxRequestNum int, duration int64, mark string) func(c *gin.Context) {
	if common.RedisEnabled {
		return func(c *gin.Context) {
			redisRateLimiter(c, maxRequestNum, duration, mark)
		}
	}
	// It's safe to call multi times.
	inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
	return func(c *gin.Context) {
		memoryRateLimiter(c, maxRequestNum, duration, mark)
	}
}

// liveRateLimitFactory 返回逐请求读取开关与阈值的限流中间件:开关/次数/窗口通过指针
// 引用 runtime_setting 镜像的全局量,热更新后无需重建路由即可生效;次数 <= 0 视为旁路。
func liveRateLimitFactory(enabled *bool, maxRequestNum *int, duration *int64, mark string) func(c *gin.Context) {
	if !common.RedisEnabled {
		// It's safe to call multi times.
		inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
	}
	return func(c *gin.Context) {
		if !*enabled || *maxRequestNum <= 0 {
			c.Next()
			return
		}
		if common.RedisEnabled {
			redisRateLimiter(c, *maxRequestNum, *duration, mark)
		} else {
			memoryRateLimiter(c, *maxRequestNum, *duration, mark)
		}
	}
}

// GlobalWebRateLimit keeps runtime threshold updates live and gives build assets
// their own budget, leaving the dashboard page budget available for navigation.
func GlobalWebRateLimit(frontendFS static.ServeFileSystem) func(c *gin.Context) {
	webLimit := liveRateLimitFactory(&common.GlobalWebRateLimitEnable, &common.GlobalWebRateLimitNum, &common.GlobalWebRateLimitDuration, "GW")
	staticLimit := liveRateLimitFactory(&common.GlobalStaticRateLimitEnable, &common.GlobalStaticRateLimitNum, &common.GlobalStaticRateLimitDuration, "GS")
	return func(c *gin.Context) {
		if frontendFS != nil && frontendFS.Exists("/", c.Request.URL.Path) {
			staticLimit(c)
			return
		}
		webLimit(c)
	}
}

func GlobalAPIRateLimit() func(c *gin.Context) {
	return liveRateLimitFactory(&common.GlobalApiRateLimitEnable, &common.GlobalApiRateLimitNum, &common.GlobalApiRateLimitDuration, "GA")
}

func CriticalRateLimit() func(c *gin.Context) {
	return liveRateLimitFactory(&common.CriticalRateLimitEnable, &common.CriticalRateLimitNum, &common.CriticalRateLimitDuration, "CT")
}

func UserCriticalRateLimit(scope string) func(c *gin.Context) {
	limit := liveUserRateLimitFactory(&common.CriticalRateLimitNum, &common.CriticalRateLimitDuration, "UC:"+scope)
	return func(c *gin.Context) {
		if !common.CriticalRateLimitEnable {
			c.Next()
			return
		}
		limit(c)
	}
}

func DownloadRateLimit() func(c *gin.Context) {
	return rateLimitFactory(common.DownloadRateLimitNum, common.DownloadRateLimitDuration, "DW")
}

func UploadRateLimit() func(c *gin.Context) {
	return rateLimitFactory(common.UploadRateLimitNum, common.UploadRateLimitDuration, "UP")
}

// userRateLimitFactory creates a rate limiter keyed by authenticated user ID
// instead of client IP, making it resistant to proxy rotation attacks.
// Must be used AFTER authentication middleware (UserAuth).
func userRateLimitFactory(maxRequestNum int, duration int64, mark string) func(c *gin.Context) {
	if common.RedisEnabled {
		return func(c *gin.Context) {
			userID := c.GetInt("id")
			if userID == 0 {
				c.Status(http.StatusUnauthorized)
				c.Abort()
				return
			}
			userRedisRateLimiter(c, maxRequestNum, duration, redisUserRateLimitKey(mark, userID))
		}
	}
	inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
	return func(c *gin.Context) {
		userID := c.GetInt("id")
		if userID == 0 {
			c.Status(http.StatusUnauthorized)
			c.Abort()
			return
		}
		key := fmt.Sprintf("%s:user:%d", mark, userID)
		if !inMemoryRateLimiter.Request(key, maxRequestNum, duration) {
			writeRateLimited(c, duration)
			return
		}
	}
}

// liveUserRateLimitFactory reads thresholds for every request so runtime
// setting updates take effect without rebuilding the router.
func liveUserRateLimitFactory(maxRequestNum *int, duration *int64, mark string) func(c *gin.Context) {
	if !common.RedisEnabled {
		inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
	}
	return func(c *gin.Context) {
		userID := c.GetInt("id")
		if userID == 0 {
			c.Status(http.StatusUnauthorized)
			c.Abort()
			return
		}
		limit := *maxRequestNum
		window := *duration
		if limit <= 0 || window <= 0 {
			return
		}
		if common.RedisEnabled {
			userRedisRateLimiter(c, limit, window, redisUserRateLimitKey(mark, userID))
			return
		}
		key := fmt.Sprintf("%s:user:%d", mark, userID)
		if !inMemoryRateLimiter.Request(key, limit, window) {
			writeRateLimited(c, window)
		}
	}
}

// userRedisRateLimiter is like redisRateLimiter but accepts a pre-built key
// (to support user-ID-based keys).
func userRedisRateLimiter(c *gin.Context, maxRequestNum int, duration int64, key string) {
	allowed, _, ttlSeconds, err := redisFixedWindowTake(c.Request.Context(), key, maxRequestNum, duration)
	if err != nil {
		logger.LogError(c.Request.Context(), common.LogText("rate limit check failed (key=%s): %v", key, err))
		c.Status(http.StatusInternalServerError)
		c.Abort()
		return
	}
	if !allowed {
		writeRateLimited(c, ttlSeconds)
	}
}

// SearchRateLimit returns a per-user rate limiter for search endpoints.
// 开关与阈值来自 runtime_setting(运行时配置),逐请求读取,热更新即时生效。
func SearchRateLimit() func(c *gin.Context) {
	limiter := liveUserRateLimitFactory(&common.SearchRateLimitNum, &common.SearchRateLimitDuration, "SR")
	return func(c *gin.Context) {
		if !common.SearchRateLimitEnable || common.SearchRateLimitNum <= 0 {
			c.Next()
			return
		}
		limiter(c)
	}
}
