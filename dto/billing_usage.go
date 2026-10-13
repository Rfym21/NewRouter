// Compatibility aliases for protocol types extracted into RelayKit.
package dto

import kit "new-router/relaykit/dto"

const BillingUsageSourceClaudeMessages = kit.BillingUsageSourceClaudeMessages
const BillingUsageSourceGeminiChat = kit.BillingUsageSourceGeminiChat
const BillingUsageSourceOAIChat = kit.BillingUsageSourceOAIChat
const BillingUsageSourceOAIResponses = kit.BillingUsageSourceOAIResponses
const BillingUsageSemanticAnthropic = kit.BillingUsageSemanticAnthropic
const BillingUsageSemanticGemini = kit.BillingUsageSemanticGemini
const BillingUsageSemanticOpenAI = kit.BillingUsageSemanticOpenAI

type BillingUsage = kit.BillingUsage

var NewClaudeMessagesBillingUsage = kit.NewClaudeMessagesBillingUsage
var HasClaudeUsageTokens = kit.HasClaudeUsageTokens
var NewOpenAIChatBillingUsage = kit.NewOpenAIChatBillingUsage
var NewOpenAIResponsesBillingUsage = kit.NewOpenAIResponsesBillingUsage
var HasOpenAIUsageTokens = kit.HasOpenAIUsageTokens
var NewGeminiChatBillingUsage = kit.NewGeminiChatBillingUsage
var NewEstimatedGeminiChatBillingUsage = kit.NewEstimatedGeminiChatBillingUsage
var CloneBillingUsageWithEstimatedCompletion = kit.CloneBillingUsageWithEstimatedCompletion
var CloneBillingUsage = kit.CloneBillingUsage
var HasGeminiUsageMetadataTokens = kit.HasGeminiUsageMetadataTokens
