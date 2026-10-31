package middleware

import (
	"fmt"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	"new-router/common"
)

func RelayPanicRecover() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				common.SysLog(common.LogText("panic detected: %v", err))
				common.SysLog(common.LogText("stacktrace from panic: %s", string(debug.Stack())))
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": gin.H{
						"message": fmt.Sprintf("Panic detected, error: %v. Please submit an issue here: https://github.com/Rfym21/WorkSpace/issues", err),
						"type":    "new_router_panic",
					},
				})
				c.Abort()
			}
		}()
		c.Next()
	}
}

// PanicRecover is the server-wide panic recovery. Gin logs the panic stack
// through common.ErrorLogWriter so it reaches the current log file.
func PanicRecover() gin.HandlerFunc {
	return gin.CustomRecoveryWithWriter(common.ErrorLogWriter, func(c *gin.Context, err any) {
		common.SysLog(common.LogText("panic detected: %v", err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"message": fmt.Sprintf("Panic detected, error: %v. Please submit an issue here: https://github.com/Rfym21/WorkSpace/issues", err),
				"type":    "new_router_panic",
			},
		})
	})
}
