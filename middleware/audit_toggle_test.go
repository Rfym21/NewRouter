package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"new-router/model"
	"new-router/setting/operation_setting"
)

func TestAuditDisabledSkipsMiddlewareCaptureWithoutAffectingHandlers(t *testing.T) {
	previousEnabled := operation_setting.IsAuditLogEnabled()
	operation_setting.SetAuditLogEnabled(false)
	t.Cleanup(func() { operation_setting.SetAuditLogEnabled(previousEnabled) })
	router := gin.New()
	router.Use(AccessTokenAudit(), TokenOperationAudit())
	router.POST("/api/token/", func(c *gin.Context) {
		beginAccessTokenAudit(c, &model.UserBase{Id: 1}, "test-ref")
		_, capturing := c.Writer.(*auditResponseWriter)
		assert.False(t, capturing)
		_, hasAccessAudit := c.Get(accessTokenAuditContextKey)
		assert.False(t, hasAccessAudit)
		finishAccessTokenAudit(c)
		c.JSON(http.StatusCreated, gin.H{"success": true, "id": 42})
	})
	request := httptest.NewRequest(http.MethodPost, "/api/token/", nil)
	request.Header.Set("Authorization", "Bearer external-panel-token")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	assert.Equal(t, http.StatusCreated, response.Code)
	assert.JSONEq(t, `{"success":true,"id":42}`, response.Body.String())
}
