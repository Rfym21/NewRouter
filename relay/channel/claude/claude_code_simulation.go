package claude

import (
	"net/http"
	"strings"

	"new-router/relay/simulation"
	"new-router/relaykit/dto"
)

func isClaudeCodeIdentityHeader(name string) bool {
	lowerName := strings.ToLower(strings.TrimSpace(name))
	return lowerName == "x-claude-code-session-id" || strings.HasPrefix(lowerName, "x-stainless-")
}

// ApplyClaudeCodeSimulationHeaders applies the full Claude Code CLI header set.
func ApplyClaudeCodeSimulationHeaders(req *http.Header, apiKey string, config *dto.ClaudeCodeSimulationConfig) {
	simulation.ApplyClaudeCodeSimulationHeaders(req, apiKey, config)
}

// ApplyClaudeCodeSimulationBetaHeader appends the Claude Code beta tokens only.
func ApplyClaudeCodeSimulationBetaHeader(req *http.Header) {
	simulation.ApplyClaudeCodeSimulationBetaHeader(req)
}

// ClaudeCodeBetaHeader returns the beta tokens required by Claude Code requests.
func ClaudeCodeBetaHeader() string {
	return simulation.ClaudeCodeBetaHeader()
}

// ApplyClaudeCodeSimulationBody applies the Claude Code CLI body shape.
func ApplyClaudeCodeSimulationBody(jsonData []byte, config *dto.ClaudeCodeSimulationConfig) ([]byte, error) {
	return simulation.ApplyClaudeCodeSimulationBody(jsonData, config)
}
