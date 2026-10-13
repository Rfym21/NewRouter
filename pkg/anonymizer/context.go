package anonymizer

import "github.com/gin-gonic/gin"

const contextKey = "new-router.anonymizer.session"

// Attach stores a session on the gin context for response processing.
func Attach(c *gin.Context, session *Session) {
	if c == nil || session == nil {
		return
	}
	c.Set(contextKey, session)
}

// FromContext returns the anonymizer session attached to the gin context.
func FromContext(c *gin.Context) *Session {
	if c == nil {
		return nil
	}
	value, ok := c.Get(contextKey)
	if !ok {
		return nil
	}
	session, _ := value.(*Session)
	return session
}
