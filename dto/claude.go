// Compatibility aliases for protocol types extracted into RelayKit.
package dto

import kit "new-router/relaykit/dto"

type ClaudeMetadata = kit.ClaudeMetadata
type ClaudeMediaMessage = kit.ClaudeMediaMessage
type ClaudeMessageSource = kit.ClaudeMessageSource
type ClaudeMessage = kit.ClaudeMessage
type Tool = kit.Tool
type InputSchema = kit.InputSchema
type ClaudeWebSearchTool = kit.ClaudeWebSearchTool
type ClaudeWebSearchUserLocation = kit.ClaudeWebSearchUserLocation
type ClaudeToolChoice = kit.ClaudeToolChoice
type ClaudeRequest = kit.ClaudeRequest
type OutputConfigForEffort = kit.OutputConfigForEffort

var ProcessTools = kit.ProcessTools

type Thinking = kit.Thinking
type ClaudeErrorWithStatusCode = kit.ClaudeErrorWithStatusCode
type ClaudeResponse = kit.ClaudeResponse
type ClaudeUsage = kit.ClaudeUsage
type ClaudeCacheCreationUsage = kit.ClaudeCacheCreationUsage
type ClaudeServerToolUse = kit.ClaudeServerToolUse
