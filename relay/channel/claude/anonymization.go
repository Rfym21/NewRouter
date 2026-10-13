package claude

import (
	"new-router/common"
	"new-router/logger"
	"new-router/pkg/anonymizer"

	"github.com/gin-gonic/gin"
)

func deanonymizeResponseBytes(c *gin.Context, data []byte) ([]byte, error) {
	session := anonymizer.FromContext(c)
	if session == nil {
		return data, nil
	}
	restored, err := anonymizer.DeanonymizeTextFields(data, session)
	if err != nil {
		// Best-effort: an unparseable chunk has no restorable content — pass it
		// through rather than aborting the stream.
		if common.DebugEnabled {
			logger.LogDebug(c, "deanonymize response skipped (unparseable): %s", err.Error())
		}
		return data, nil
	}
	return restored, nil
}

func deanonymizeResponseString(c *gin.Context, data string) (string, error) {
	if data == "" {
		return data, nil
	}
	out, err := deanonymizeResponseBytes(c, []byte(data))
	if err != nil {
		return data, err
	}
	return string(out), nil
}
