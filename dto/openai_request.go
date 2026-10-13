// Compatibility aliases for protocol types extracted into RelayKit.
package dto

import kit "new-router/relaykit/dto"

type ResponseFormat = kit.ResponseFormat
type FormatJsonSchema = kit.FormatJsonSchema
type GeneralOpenAIRequest = kit.GeneralOpenAIRequest

var IsOpenAIReasoningOModel = kit.IsOpenAIReasoningOModel
var IsOpenAIGPT5Model = kit.IsOpenAIGPT5Model

type OpenAIChatCapabilities = kit.OpenAIChatCapabilities

var GetOpenAIChatCapabilities = kit.GetOpenAIChatCapabilities
var IsQwenThinkingBudgetModel = kit.IsQwenThinkingBudgetModel

const CustomType = kit.CustomType

type ToolCallRequest = kit.ToolCallRequest
type FunctionRequest = kit.FunctionRequest
type StreamOptions = kit.StreamOptions
type Message = kit.Message
type MediaContent = kit.MediaContent
type MessageImageUrl = kit.MessageImageUrl
type MessageInputAudio = kit.MessageInputAudio
type MessageFile = kit.MessageFile
type MessageVideoUrl = kit.MessageVideoUrl

const ContentTypeText = kit.ContentTypeText
const ContentTypeImageURL = kit.ContentTypeImageURL
const ContentTypeInputAudio = kit.ContentTypeInputAudio
const ContentTypeFile = kit.ContentTypeFile
const ContentTypeVideoUrl = kit.ContentTypeVideoUrl

type WebSearchOptions = kit.WebSearchOptions
type OpenAIResponsesRequest = kit.OpenAIResponsesRequest
type Reasoning = kit.Reasoning
type Input = kit.Input
type MediaInput = kit.MediaInput
