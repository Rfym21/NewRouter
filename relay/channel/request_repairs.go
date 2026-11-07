package channel

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"new-router/logger"
	"new-router/relay/channel/requestrepair"
	relaycommon "new-router/relay/common"
	"new-router/service"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// responsePreview keeps the transport body open while replaying inspected bytes.
type responsePreview struct {
	io.Reader
	io.Closer
}

func doRequestWithRepairs(c *gin.Context, client *http.Client, req *http.Request, info *relaycommon.RelayInfo, rebuild func([]byte) (*http.Request, error)) (*http.Response, error) {
	started := time.Now()
	settings := info.ChannelOtherSettings
	imageEnabled := info.ChannelSetting.ImageFallbackEnabled
	if req.Body == nil || req.Method != http.MethodPost ||
		(!imageEnabled && !settings.ThinkingSignatureRectifierEnabled && !settings.ResponsesEncryptedContentRectifierEnabled) {
		return client.Do(req)
	}
	// Repair only JSON message requests. Multipart uploads and other API payloads
	// retain their streaming and replay behavior.
	contentType := strings.ToLower(req.Header.Get("Content-Type"))
	if contentType != "" && !strings.Contains(contentType, "application/json") {
		return client.Do(req)
	}
	if req.GetBody == nil {
		// The usual outbound body already provides GetBody via BodyStorage.
		// Buffer only callers that cannot otherwise replay a rejected request.
		body, err := io.ReadAll(io.LimitReader(req.Body, (64<<20)+1))
		if err != nil {
			if closeErr := req.Body.Close(); closeErr != nil {
				logger.LogWarn(c, "close unreadable request: "+closeErr.Error())
			}
			return nil, fmt.Errorf("buffer request for repair: %w", err)
		}
		if len(body) > 64<<20 {
			req.Body = &responsePreview{Reader: io.MultiReader(bytes.NewReader(body), req.Body), Closer: req.Body}
			return client.Do(req)
		}
		if err := req.Body.Close(); err != nil {
			return nil, fmt.Errorf("close buffered request: %w", err)
		}
		req = requestWithBody(req, body)
	}
	var body []byte
	bodyReady := false
	imageTried, thinkingTried := false, false
	attemptClient := *client
	for {
		if client.Timeout > 0 {
			// Share the original HTTP timeout across repair attempts and body reads.
			attemptClient.Timeout = client.Timeout - time.Since(started)
			if attemptClient.Timeout <= 0 {
				return nil, context.DeadlineExceeded
			}
		}
		resp, err := attemptClient.Do(req)
		if err != nil || resp == nil || resp.Body == nil {
			return resp, err
		}
		thinkingStatus := resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnprocessableEntity
		imageStatus := requestrepair.IsImageFallbackStatus(resp.StatusCode)
		if (!imageEnabled || imageTried || !imageStatus) &&
			(thinkingTried || !thinkingStatus || (!settings.ThinkingSignatureRectifierEnabled && !settings.ResponsesEncryptedContentRectifierEnabled)) {
			return resp, nil
		}
		// Bound inspection independently of the original response. A truncated or
		// unreadable error remains consumable by the normal response handler.
		preview, readErr := io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
		resp.Body = &responsePreview{Reader: io.MultiReader(bytes.NewReader(preview), resp.Body), Closer: resp.Body}
		if readErr != nil {
			logger.LogWarn(c, "inspect response for request repair: "+readErr.Error())
			return resp, nil
		}
		if len(preview) > 64<<10 {
			return resp, nil
		}
		if !bodyReady {
			replay, replayErr := req.GetBody()
			if replayErr != nil {
				logger.LogWarn(c, "open request for repair: "+replayErr.Error())
				return resp, nil
			}
			body, replayErr = service.ReadBufferedResponseBody(replay)
			closeErr := replay.Close()
			if replayErr != nil || closeErr != nil {
				logger.LogWarn(c, fmt.Sprintf("read request for repair: read=%v close=%v", replayErr, closeErr))
				return resp, nil
			}
			bodyReady = true
			if !gjson.ValidBytes(body) {
				return resp, nil
			}
		}
		var repaired []byte
		changed := false
		if !thinkingTried && thinkingStatus {
			repaired, changed = requestrepair.RepairThinking(body, preview, settings.ThinkingSignatureRectifierEnabled, settings.ResponsesEncryptedContentRectifierEnabled)
			if changed {
				thinkingTried = true
			}
		}
		if !changed && imageEnabled && !imageTried && imageStatus {
			repaired, changed, err = requestrepair.RepairImages(body, resp.StatusCode, preview)
			if err != nil {
				logger.LogWarn(c, "image fallback rewrite failed: "+err.Error())
				return resp, nil
			}
			if changed {
				imageTried = true
			}
		}
		if !changed {
			return resp, nil
		}
		if err := c.Request.Context().Err(); err != nil {
			return resp, nil
		}
		nextRequest := requestWithBody(req, repaired)
		if rebuild != nil {
			nextRequest, err = rebuild(repaired)
			if nextRequest == nil && err == nil {
				err = fmt.Errorf("request body rebuilder returned no request")
			}
			if err != nil {
				if closeErr := resp.Body.Close(); closeErr != nil {
					logger.LogWarn(c, "close response after request rebuild failure: "+closeErr.Error())
				}
				return nil, fmt.Errorf("rebuild repaired request: %w", err)
			}
			nextRequest = nextRequest.WithContext(req.Context())
		}
		if err := resp.Body.Close(); err != nil {
			logger.LogWarn(c, "close response before request repair: "+err.Error())
		}
		logger.LogDebug(c, "retrying repaired upstream request: image=%t thinking=%t", imageTried, thinkingTried)
		body = repaired
		req = nextRequest
	}
}

func requestWithBody(req *http.Request, body []byte) *http.Request {
	replay := req.Clone(req.Context())
	replay.Body = io.NopCloser(bytes.NewReader(body))
	replay.ContentLength = int64(len(body))
	replay.Header.Del("Content-Length")
	replay.TransferEncoding = nil
	replay.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	return replay
}
