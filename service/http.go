package service

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"new-router/common"
	"new-router/logger"

	"github.com/gin-gonic/gin"
)

const maxBufferedResponseBodyBytes = 64 << 20

// ErrUpstreamResponseBodyTooLarge is returned when a buffered upstream response exceeds the configured cap.
var ErrUpstreamResponseBodyTooLarge = errors.New("upstream response body too large")

// ReadBufferedResponseBody reads an upstream text/JSON body (or a rewindable request body) with a hard cap.
func ReadBufferedResponseBody(body io.Reader) ([]byte, error) {
	return ReadBufferedResponseBodyWithLimit(body, maxBufferedResponseBodyBytes)
}

// ReadBufferedResponseBodyWithLimit reads limit+1 bytes so oversized bodies fail explicitly.
func ReadBufferedResponseBodyWithLimit(body io.Reader, limit int64) ([]byte, error) {
	if body == nil {
		return nil, nil
	}
	if limit <= 0 {
		return io.ReadAll(body)
	}
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: limit=%d", ErrUpstreamResponseBodyTooLarge, limit)
	}
	return data, nil
}

func CloseResponseBodyGracefully(httpResponse *http.Response) {
	if httpResponse == nil || httpResponse.Body == nil {
		return
	}
	err := httpResponse.Body.Close()
	if err != nil {
		common.SysError(common.LogText("failed to close response body: %s", err.Error()))
	}
}

// ShouldCopyUpstreamHeader checks whether a given upstream response header
// should be copied to the client response. It returns false for Content-Length
// (managed separately) and X-Oneapi-Request-Id (to preserve the local instance
// ID). When the upstream header is X-Oneapi-Request-Id, the value is captured
// into the Gin context for later logging.
func ShouldCopyUpstreamHeader(c *gin.Context, k string, v []string) bool {
	if strings.EqualFold(k, "Content-Length") {
		return false
	}
	if strings.EqualFold(k, common.RequestIdKey) {
		if c != nil && len(v) > 0 {
			c.Set(common.UpstreamRequestIdKey, v[0])
		}
		return false
	}
	return true
}

func IOCopyBytesGracefully(c *gin.Context, src *http.Response, data []byte) {
	if c.Writer == nil {
		return
	}

	body := io.NopCloser(bytes.NewBuffer(data))

	// We shouldn't set the header before we parse the response body, because the parse part may fail.
	// And then we will have to send an error response, but in this case, the header has already been set.
	// So the httpClient will be confused by the response.
	// For example, Postman will report error, and we cannot check the response at all.
	if src != nil {
		for k, v := range src.Header {
			if !ShouldCopyUpstreamHeader(c, k, v) {
				continue
			}
			c.Writer.Header().Set(k, v[0])
		}
	}

	// set Content-Length header manually BEFORE calling WriteHeader
	c.Writer.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))

	// Write header with status code (this sends the headers)
	if src != nil {
		c.Writer.WriteHeader(src.StatusCode)
	} else {
		c.Writer.WriteHeader(http.StatusOK)
	}

	_, err := io.Copy(c.Writer, body)
	if err != nil {
		logger.LogError(c, common.LogText("failed to copy response body: %s", err.Error()))
	}
	c.Writer.Flush()
}
