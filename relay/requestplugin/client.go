package requestplugin

import (
	"net/http"
	"strings"

	"new-router/pkg/jsplugin"

	"github.com/gin-gonic/gin"
)

// clientFingerprint reads the incoming client's own identity from the request
// headers the host received, before any adaptor replaced them. A plugin uses
// it to keep a real Claude Code or Codex client's fingerprint instead of
// injecting the simulated one. Only non-credential identity headers are
// copied; the whitelist is deliberately narrow.
func clientFingerprint(c *gin.Context) *jsplugin.TransformClient {
	if c == nil || c.Request == nil {
		return nil
	}
	header := c.Request.Header
	kind := clientKind(header)
	headers := clientIdentityHeaders(header, kind)
	if kind == "" && len(headers) == 0 {
		return nil
	}
	return &jsplugin.TransformClient{Kind: kind, Headers: headers}
}

// Client kind names a request processing plugin may see.
const (
	clientKindClaudeCode = "claude_code"
	clientKindCodex      = "codex"
)

func clientKind(header http.Header) string {
	if header.Get("X-Claude-Code-Session-Id") != "" {
		return clientKindClaudeCode
	}
	userAgent := strings.ToLower(header.Get("User-Agent"))
	if strings.Contains(userAgent, "claude-cli") || strings.Contains(userAgent, "claude-code") {
		return clientKindClaudeCode
	}
	if strings.Contains(strings.ToLower(header.Get("anthropic-beta")), "claude-code-") {
		return clientKindClaudeCode
	}
	if strings.Contains(strings.ToLower(header.Get("originator")), "codex") ||
		strings.Contains(userAgent, "codex") {
		return clientKindCodex
	}
	for _, name := range []string{
		"X-Codex-Window-Id",
		"X-Codex-Turn-Metadata",
		"X-Codex-Beta-Features",
		"X-Codex-Parent-Thread-Id",
	} {
		if header.Get(name) != "" {
			return clientKindCodex
		}
	}
	return ""
}

// clientIdentityHeaders returns the identity headers of a recognized client,
// keyed in lowercase. anthropic-beta is included for every request: a plugin
// that adds beta tokens must not drop the client's own list.
func clientIdentityHeaders(header http.Header, kind string) map[string]string {
	headers := map[string]string{}
	add := func(name string) {
		if value := strings.TrimSpace(strings.Join(header.Values(name), ",")); value != "" {
			headers[strings.ToLower(name)] = value
		}
	}
	add("anthropic-beta")
	switch kind {
	case clientKindClaudeCode:
		for _, name := range []string{
			"User-Agent",
			"X-App",
			"X-Claude-Code-Session-Id",
			"Anthropic-Version",
			"Anthropic-Dangerous-Direct-Browser-Access",
		} {
			add(name)
		}
		for name := range header {
			if strings.HasPrefix(strings.ToLower(name), "x-stainless-") {
				add(name)
			}
		}
	case clientKindCodex:
		for _, name := range []string{
			"User-Agent",
			"Originator",
			"Session-Id",
			"Thread-Id",
			"X-Client-Request-Id",
			"X-Codex-Window-Id",
			"X-Codex-Turn-Metadata",
			"X-Codex-Beta-Features",
			"X-Codex-Installation-Id",
			"X-Codex-Parent-Thread-Id",
		} {
			add(name)
		}
	}
	if len(headers) == 0 {
		return nil
	}
	return headers
}
