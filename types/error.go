// Compatibility aliases for protocol types extracted into RelayKit.
package types

import kit "new-router/relaykit/types"

type OpenAIError = kit.OpenAIError
type ClaudeError = kit.ClaudeError
type ErrorType = kit.ErrorType

const ErrorTypeNewRouterError = kit.ErrorTypeNewRouterError
const ErrorTypeNewAPIError = kit.ErrorTypeNewAPIError
const ErrorTypeOpenAIError = kit.ErrorTypeOpenAIError
const ErrorTypeClaudeError = kit.ErrorTypeClaudeError
const ErrorTypeMidjourneyError = kit.ErrorTypeMidjourneyError
const ErrorTypeGeminiError = kit.ErrorTypeGeminiError
const ErrorTypeRerankError = kit.ErrorTypeRerankError
const ErrorTypeUpstreamError = kit.ErrorTypeUpstreamError

type ErrorCode = kit.ErrorCode

const ErrorCodeInvalidRequest = kit.ErrorCodeInvalidRequest
const ErrorCodeSensitiveWordsDetected = kit.ErrorCodeSensitiveWordsDetected
const ErrorCodeViolationFeeGrokCSAM = kit.ErrorCodeViolationFeeGrokCSAM
const ErrorCodeCountTokenFailed = kit.ErrorCodeCountTokenFailed
const ErrorCodeModelPriceError = kit.ErrorCodeModelPriceError
const ErrorCodeInvalidApiType = kit.ErrorCodeInvalidApiType
const ErrorCodeJsonMarshalFailed = kit.ErrorCodeJsonMarshalFailed
const ErrorCodeDoRequestFailed = kit.ErrorCodeDoRequestFailed
const ErrorCodeGetChannelFailed = kit.ErrorCodeGetChannelFailed
const ErrorCodeGenRelayInfoFailed = kit.ErrorCodeGenRelayInfoFailed
const ErrorCodeChannelNoAvailableKey = kit.ErrorCodeChannelNoAvailableKey
const ErrorCodeChannelParamOverrideInvalid = kit.ErrorCodeChannelParamOverrideInvalid
const ErrorCodeChannelHeaderOverrideInvalid = kit.ErrorCodeChannelHeaderOverrideInvalid
const ErrorCodeChannelModelMappedError = kit.ErrorCodeChannelModelMappedError
const ErrorCodeChannelAwsClientError = kit.ErrorCodeChannelAwsClientError
const ErrorCodeChannelInvalidKey = kit.ErrorCodeChannelInvalidKey
const ErrorCodeChannelResponseTimeExceeded = kit.ErrorCodeChannelResponseTimeExceeded
const ErrorCodeReadRequestBodyFailed = kit.ErrorCodeReadRequestBodyFailed
const ErrorCodeConvertRequestFailed = kit.ErrorCodeConvertRequestFailed
const ErrorCodeAccessDenied = kit.ErrorCodeAccessDenied
const ErrorCodeBadRequestBody = kit.ErrorCodeBadRequestBody
const ErrorCodeReadResponseBodyFailed = kit.ErrorCodeReadResponseBodyFailed
const ErrorCodeBadResponseStatusCode = kit.ErrorCodeBadResponseStatusCode
const ErrorCodeBadResponse = kit.ErrorCodeBadResponse
const ErrorCodeBadResponseBody = kit.ErrorCodeBadResponseBody
const ErrorCodeEmptyResponse = kit.ErrorCodeEmptyResponse
const ErrorCodeAwsInvokeError = kit.ErrorCodeAwsInvokeError
const ErrorCodeModelNotFound = kit.ErrorCodeModelNotFound
const ErrorCodePromptBlocked = kit.ErrorCodePromptBlocked
const ErrorCodeQueryDataError = kit.ErrorCodeQueryDataError
const ErrorCodeUpdateDataError = kit.ErrorCodeUpdateDataError
const ErrorCodeInsufficientUserQuota = kit.ErrorCodeInsufficientUserQuota
const ErrorCodePreConsumeTokenQuotaFailed = kit.ErrorCodePreConsumeTokenQuotaFailed

type NewAPIError = kit.NewAPIError
type NewAPIErrorOptions = kit.NewAPIErrorOptions

var NewError = kit.NewError
var NewOpenAIError = kit.NewOpenAIError
var InitOpenAIError = kit.InitOpenAIError
var NewErrorWithStatusCode = kit.NewErrorWithStatusCode
var WithOpenAIError = kit.WithOpenAIError
var WithClaudeError = kit.WithClaudeError
var IsChannelError = kit.IsChannelError
var IsSkipRetryError = kit.IsSkipRetryError
var ErrOptionWithSkipRetry = kit.ErrOptionWithSkipRetry
var ErrOptionWithSensitiveWordsDisclosure = kit.ErrOptionWithSensitiveWordsDisclosure
var ErrOptionWithNoRecordErrorLog = kit.ErrOptionWithNoRecordErrorLog
var ErrOptionWithStatusCode = kit.ErrOptionWithStatusCode
var ErrOptionWithHideErrMsg = kit.ErrOptionWithHideErrMsg
var IsRecordErrorLog = kit.IsRecordErrorLog
