package middleware

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"new-router/common"
	"new-router/constant"
	"new-router/i18n"
	relaycommon "new-router/relay/common"
)

func TestModelRedisRateLimitUsesUTCRegardlessOfLocalTimezone(t *testing.T) {
	redisServer, redisClient := useRateLimitMiniRedis(t)
	previousLocation := time.Local
	time.Local = time.FixedZone("test-utc-plus-eight", 8*60*60)
	t.Cleanup(func() { time.Local = previousLocation })

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	redisServer.SetTime(now)
	key := "rateLimit:model-utc-reservation"
	allowed, member, err := redisSlidingWindowReserve(ctx, key, 1, 60, time.Minute)
	require.NoError(t, err)
	require.True(t, allowed)
	score, err := redisClient.ZScore(ctx, key, member).Result()
	require.NoError(t, err)
	assert.Equal(t, float64(now.UnixMilli()), score)

	redisServer.SetTime(now.Add(30 * time.Second))
	allowed, _, err = redisSlidingWindowReserve(ctx, key, 1, 60, time.Minute)
	require.NoError(t, err)
	assert.False(t, allowed, "a reservation inside the window remains limited on a non-UTC host")

	redisServer.SetTime(now.Add(61 * time.Second))
	allowed, _, err = redisSlidingWindowReserve(ctx, key, 1, 60, time.Minute)
	require.NoError(t, err)
	assert.True(t, allowed)
}

var modelRateLimitTestUsers atomic.Int64

func TestModelRateLimitStreamFailuresDoNotConsumeSuccessLimit(t *testing.T) {
	require.NoError(t, i18n.Init())
	for _, backend := range []string{"memory", "redis"} {
		for _, totalLimit := range []int{0, 2} {
			t.Run(fmt.Sprintf("%s/total=%d", backend, totalLimit), func(t *testing.T) {
				userID := 7200000 + int(modelRateLimitTestUsers.Add(1))
				handler := func(c *gin.Context) { applyMemoryModelRateLimit(c, 60, totalLimit, 1) }
				if backend == "redis" {
					useRateLimitMiniRedis(t)
					handler = func(c *gin.Context) { applyRedisModelRateLimit(c, 60, totalLimit, 1) }
				}
				router := gin.New()
				router.GET("/:outcome", func(c *gin.Context) { c.Set("id", userID) }, handler, func(c *gin.Context) {
					status := relaycommon.NewStreamStatus()
					if c.Param("outcome") == "failed" {
						status.MarkFailed("server_error", "", 0)
					} else {
						status.MarkCompleted()
					}
					common.SetContextKey(c, constant.ContextKeyResponseStreamStatus, status)
					c.Status(http.StatusOK)
				})
				assert.Equal(t, http.StatusOK, performRateLimitRequest(router, "/failed", "127.0.0.1:1000").Code)
				if totalLimit > 0 {
					assert.Equal(t, http.StatusOK, performRateLimitRequest(router, "/failed", "127.0.0.1:1000").Code)
				} else {
					assert.Equal(t, http.StatusOK, performRateLimitRequest(router, "/completed", "127.0.0.1:1000").Code)
				}
				assert.Equal(t, http.StatusTooManyRequests, performRateLimitRequest(router, "/completed", "127.0.0.1:1000").Code)
			})
		}
	}
}

func TestModelMemoryRateLimitReservesConcurrentSuccessAdmission(t *testing.T) {
	require.NoError(t, i18n.Init())
	userID := 7200000 + int(modelRateLimitTestUsers.Add(1))
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	router := gin.New()
	router.GET("/:outcome", func(c *gin.Context) { c.Set("id", userID) }, func(c *gin.Context) { applyMemoryModelRateLimit(c, 60, 0, 1) }, func(c *gin.Context) {
		if c.Param("outcome") == "failed" {
			close(entered)
			<-release
			status := relaycommon.NewStreamStatus()
			status.MarkFailed("server_error", "", 0)
			common.SetContextKey(c, constant.ContextKeyResponseStreamStatus, status)
		}
		c.Status(http.StatusOK)
	})
	go func() {
		defer close(finished)
		assert.Equal(t, http.StatusOK, performRateLimitRequest(router, "/failed", "127.0.0.1:1000").Code)
	}()
	<-entered
	assert.Equal(t, http.StatusTooManyRequests, performRateLimitRequest(router, "/completed", "127.0.0.1:1000").Code)
	close(release)
	<-finished
	assert.Equal(t, http.StatusOK, performRateLimitRequest(router, "/completed", "127.0.0.1:1000").Code)
	assert.Equal(t, http.StatusTooManyRequests, performRateLimitRequest(router, "/completed", "127.0.0.1:1000").Code)
}

// The memory limiter answers like the Redis limiter: an OpenAI error body
// naming the limit that was reached, not an empty 429.
func TestModelMemoryRateLimitRejectionsReturnOpenAIError(t *testing.T) {
	require.NoError(t, i18n.Init())
	for _, tc := range []struct {
		name                     string
		totalLimit, successLimit int
		messageKey               string
	}{
		{name: "total", totalLimit: 1, messageKey: i18n.MsgRateLimitTotalReached},
		{name: "success", successLimit: 1, messageKey: i18n.MsgRateLimitReached},
	} {
		t.Run(tc.name, func(t *testing.T) {
			userID := 7200000 + int(modelRateLimitTestUsers.Add(1))
			router := gin.New()
			router.GET("/", func(c *gin.Context) {
				c.Set("id", userID)
				c.Set(common.RequestIdKey, "req-limited")
			}, func(c *gin.Context) {
				applyMemoryModelRateLimit(c, 60, tc.totalLimit, tc.successLimit)
			}, func(c *gin.Context) {
				c.Status(http.StatusOK)
			})
			require.Equal(t, http.StatusOK, performRateLimitRequest(router, "/", "127.0.0.1:1000").Code)

			resp := performRateLimitRequest(router, "/", "127.0.0.1:1000")
			assert.Equal(t, http.StatusTooManyRequests, resp.Code)
			message := i18n.Translate("", tc.messageKey, map[string]any{"Minutes": 1, "Max": 1})
			want, err := common.Marshal(gin.H{"error": gin.H{
				"message": common.MessageWithRequestId(message, "req-limited"),
				"type":    "new_router_error",
				"code":    "",
			}})
			require.NoError(t, err)
			assert.JSONEq(t, string(want), resp.Body.String())
		})
	}
}
