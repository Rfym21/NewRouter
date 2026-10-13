package middleware

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"new-router/common"
)

const RouteTagKey = "route_tag"

func RouteTag(tag string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(RouteTagKey, tag)
		c.Next()
	}
}

// SetUpLogger 安装访问日志中间件。日志函数固定安装,是否输出由
// common.AccessLogEnabled 逐请求判定,以支持运行时配置热切换。
func SetUpLogger(server *gin.Engine) {
	server.Use(redactTaskArtifactAccessQuery())
	accessLogger := gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		var requestID string
		if param.Keys != nil {
			requestID, _ = param.Keys[common.RequestIdKey].(string)
		}
		tag, _ := param.Keys[RouteTagKey].(string)
		if tag == "" {
			tag = "web"
		}
		path := param.Path
		// OAuth callbacks carry one-time codes and state in the query string.
		// Redact the log value only; the handler still needs the original query.
		if strings.HasPrefix(path, "/api/oauth/") || strings.HasPrefix(path, "/oauth/") {
			path, _, _ = strings.Cut(path, "?")
		}
		return fmt.Sprintf("[GIN] %s | %s | %s | %3d | %13v | %15s | %7s %s\n",
			param.TimeStamp.Format("2006/01/02 - 15:04:05"),
			tag,
			requestID,
			param.StatusCode,
			param.Latency,
			param.ClientIP,
			param.Method,
			path,
		)
	})
	server.Use(func(c *gin.Context) {
		if !common.AccessLogEnabled {
			c.Next()
			return
		}
		accessLogger(c)
	})
}
