// Compatibility aliases for protocol types extracted into RelayKit.
package types

import kit "new-router/relaykit/types"

type EndpointType = kit.EndpointType

const EndpointTypeOpenAI = kit.EndpointTypeOpenAI
const EndpointTypeOpenAIResponse = kit.EndpointTypeOpenAIResponse
const EndpointTypeOpenAIResponseCompact = kit.EndpointTypeOpenAIResponseCompact
const EndpointTypeOpenAIAlphaSearch = kit.EndpointTypeOpenAIAlphaSearch
const EndpointTypeAnthropic = kit.EndpointTypeAnthropic
const EndpointTypeGemini = kit.EndpointTypeGemini
const EndpointTypeJinaRerank = kit.EndpointTypeJinaRerank
const EndpointTypeImageGeneration = kit.EndpointTypeImageGeneration
const EndpointTypeEmbeddings = kit.EndpointTypeEmbeddings
const EndpointTypeOpenAIVideo = kit.EndpointTypeOpenAIVideo

var FinishReasonStop = kit.FinishReasonStop
var FinishReasonToolCalls = kit.FinishReasonToolCalls
var FinishReasonLength = kit.FinishReasonLength
var FinishReasonFunctionCall = kit.FinishReasonFunctionCall
var FinishReasonContentFilter = kit.FinishReasonContentFilter
