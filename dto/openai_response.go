// Compatibility aliases for protocol types extracted into RelayKit.
package dto

import kit "new-router/relaykit/dto"

const ResponsesOutputTypeImageGenerationCall = kit.ResponsesOutputTypeImageGenerationCall

type SimpleResponse = kit.SimpleResponse
type TextResponse = kit.TextResponse
type OpenAITextResponseChoice = kit.OpenAITextResponseChoice
type OpenAITextResponse = kit.OpenAITextResponse
type OpenAIEmbeddingResponseItem = kit.OpenAIEmbeddingResponseItem
type OpenAIEmbeddingResponse = kit.OpenAIEmbeddingResponse
type FlexibleEmbeddingResponseItem = kit.FlexibleEmbeddingResponseItem
type FlexibleEmbeddingResponse = kit.FlexibleEmbeddingResponse
type ChatCompletionsStreamResponseChoice = kit.ChatCompletionsStreamResponseChoice
type ChatCompletionsStreamResponseChoiceDelta = kit.ChatCompletionsStreamResponseChoiceDelta
type ToolCallResponse = kit.ToolCallResponse
type FunctionResponse = kit.FunctionResponse
type ChatCompletionsStreamResponse = kit.ChatCompletionsStreamResponse
type ChatCompletionsStreamResponseSimple = kit.ChatCompletionsStreamResponseSimple
type CompletionsStreamResponse = kit.CompletionsStreamResponse
type Usage = kit.Usage
type OpenAIVideoResponse = kit.OpenAIVideoResponse
type InputTokenDetails = kit.InputTokenDetails
type OutputTokenDetails = kit.OutputTokenDetails
type OpenAIResponsesResponse = kit.OpenAIResponsesResponse
type IncompleteDetails = kit.IncompleteDetails
type ResponsesOutput = kit.ResponsesOutput

var NormalizeResponsesWebSearchAction = kit.NormalizeResponsesWebSearchAction
var ResponsesArgumentsString = kit.ResponsesArgumentsString

type ResponsesOutputContent = kit.ResponsesOutputContent
type ResponsesReasoningSummaryPart = kit.ResponsesReasoningSummaryPart

const BuildInToolWebSearchPreview = kit.BuildInToolWebSearchPreview
const BuildInToolWebSearch = kit.BuildInToolWebSearch
const BuildInToolFileSearch = kit.BuildInToolFileSearch
const BuildInToolGoogleSearch = kit.BuildInToolGoogleSearch
const BuildInToolImageGeneration = kit.BuildInToolImageGeneration
const BuildInCallWebSearchCall = kit.BuildInCallWebSearchCall
const BuildInCallFileSearchCall = kit.BuildInCallFileSearchCall
const BuildInCallFunctionCall = kit.BuildInCallFunctionCall
const BuildInCallToolUse = kit.BuildInCallToolUse
const ResponsesOutputTypeItemAdded = kit.ResponsesOutputTypeItemAdded
const ResponsesOutputTypeItemDone = kit.ResponsesOutputTypeItemDone

type ResponsesStreamResponse = kit.ResponsesStreamResponse

var GetOpenAIError = kit.GetOpenAIError
