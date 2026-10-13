package middleware

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"new-router/common"
	"new-router/common/limiter"
	"new-router/constant"
	"new-router/i18n"
	relaycommon "new-router/relay/common"
	"new-router/setting"

	"github.com/gin-gonic/gin"
)

const (
	// ModelRequestRateLimitCountMark identifies total-request counters.
	ModelRequestRateLimitCountMark = "MRRL"
	// ModelRequestRateLimitSuccessCountMark identifies successful-request counters.
	ModelRequestRateLimitSuccessCountMark = "MRRLS"
)

func applyRedisModelRateLimit(c *gin.Context, duration int64, totalMaxCount, successMaxCount int) {
	userID := strconv.Itoa(c.GetInt("id"))
	ctx, cancel := redisRateLimitContext()
	defer cancel()

	successKey := fmt.Sprintf("rateLimit:%s:%s", ModelRequestRateLimitSuccessCountMark, userID)
	successAllowed, successMember, err := redisSlidingWindowReserve(
		ctx,
		successKey,
		successMaxCount,
		duration,
		time.Duration(duration)*time.Second,
	)
	if err != nil {
		common.SysError(common.LogText("failed to check the successful request limit: %s", err.Error()))
		abortWithOpenAiMessage(c, http.StatusInternalServerError, "rate_limit_check_failed")
		return
	}
	if !successAllowed {
		abortWithOpenAiMessage(c, http.StatusTooManyRequests, i18n.T(c, i18n.MsgRateLimitReached, map[string]any{"Minutes": duration / 60, "Max": successMaxCount}))
		return
	}

	if totalMaxCount > 0 {
		totalKey := fmt.Sprintf("rateLimit:%s", userID)
		tokenBucket := limiter.New(ctx, common.RDB)
		allowed, err := tokenBucket.Allow(
			ctx,
			totalKey,
			limiter.WithCapacity(rateLimitCapacity(totalMaxCount, duration)),
			limiter.WithRate(int64(totalMaxCount)),
			limiter.WithRequested(duration),
		)
		if err != nil {
			releaseRedisSlidingWindow(successKey, successMember)
			common.SysError(common.LogText("failed to check the total request limit: %s", err.Error()))
			abortWithOpenAiMessage(c, http.StatusInternalServerError, "rate_limit_check_failed")
			return
		}
		if !allowed {
			releaseRedisSlidingWindow(successKey, successMember)
			abortWithOpenAiMessage(c, http.StatusTooManyRequests, i18n.T(c, i18n.MsgRateLimitTotalReached, map[string]any{"Minutes": duration / 60, "Max": totalMaxCount}))
			return
		}
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			releaseRedisSlidingWindow(successKey, successMember)
			panic(recovered)
		}
		if !modelRequestSucceeded(c) {
			releaseRedisSlidingWindow(successKey, successMember)
		}
	}()
	c.Next()
}

func applyMemoryModelRateLimit(c *gin.Context, duration int64, totalMaxCount, successMaxCount int) {
	inMemoryRateLimiter.Init(time.Duration(duration) * time.Second)
	userID := strconv.Itoa(c.GetInt("id"))
	totalKey := ModelRequestRateLimitCountMark + userID
	successKey := ModelRequestRateLimitSuccessCountMark + userID

	if totalMaxCount > 0 && !inMemoryRateLimiter.Request(totalKey, totalMaxCount, duration) {
		abortWithOpenAiMessage(c, http.StatusTooManyRequests, i18n.T(c, i18n.MsgRateLimitTotalReached, map[string]any{"Minutes": duration / 60, "Max": totalMaxCount}))
		return
	}
	var reservation *common.RateLimitReservation
	if successMaxCount > 0 {
		reservation = inMemoryRateLimiter.Reserve(successKey, successMaxCount, duration)
		if reservation == nil {
			abortWithOpenAiMessage(c, http.StatusTooManyRequests, i18n.T(c, i18n.MsgRateLimitReached, map[string]any{"Minutes": duration / 60, "Max": successMaxCount}))
			return
		}
		defer reservation.Complete(false)
	}
	c.Next()
	reservation.Complete(modelRequestSucceeded(c))
}

func modelRequestSucceeded(c *gin.Context) bool {
	status, _ := common.GetContextKeyType[*relaycommon.StreamStatus](c, constant.ContextKeyResponseStreamStatus)
	return c.Writer.Status() < http.StatusBadRequest && !status.ResponseFailed()
}

// ModelRequestRateLimit applies per-user model request limits.
func ModelRequestRateLimit() func(c *gin.Context) {
	return func(c *gin.Context) {
		if !setting.ModelRequestRateLimitEnabled {
			c.Next()
			return
		}

		durationMinutes := setting.ModelRequestRateLimitDurationMinutes
		if durationMinutes < 1 {
			durationMinutes = 1
		}
		duration := rateLimitDurationSeconds(durationMinutes)
		totalMaxCount := setting.ModelRequestRateLimitCount
		successMaxCount := setting.ModelRequestRateLimitSuccessCount
		if successMaxCount < 1 {
			successMaxCount = 1000
		}

		userID := c.GetInt("id")
		group := common.GetContextKeyString(c, constant.ContextKeyTokenGroup)
		if group == "" {
			group = common.GetContextKeyString(c, constant.ContextKeyUserGroup)
		}

		groupTotalCount, groupSuccessCount, found := setting.GetGroupRateLimit(group)
		if found {
			totalMaxCount = groupTotalCount
			successMaxCount = groupSuccessCount
		}

		userTotalCount, userSuccessCount, found := setting.GetUserRateLimit(userID, group)
		if found {
			totalMaxCount = userTotalCount
			successMaxCount = userSuccessCount
		}

		if common.RedisEnabled {
			applyRedisModelRateLimit(c, duration, totalMaxCount, successMaxCount)
			return
		}
		applyMemoryModelRateLimit(c, duration, totalMaxCount, successMaxCount)
	}
}

func rateLimitDurationSeconds(durationMinutes int) int64 {
	if durationMinutes <= 0 {
		return 0
	}
	minutes := int64(durationMinutes)
	if minutes > math.MaxInt64/60 {
		return math.MaxInt64
	}
	return minutes * 60
}

func rateLimitCapacity(count int, durationSeconds int64) int64 {
	if count <= 0 || durationSeconds <= 0 {
		return 0
	}
	c := int64(count)
	if c > math.MaxInt64/durationSeconds {
		return math.MaxInt64
	}
	return c * durationSeconds
}
