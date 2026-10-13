package relay

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
	"new-router/common"
	"new-router/constant"
	"new-router/logger"
	relaycommon "new-router/relay/common"
	"new-router/relay/helper"
	"new-router/relaykit/dto"
	"new-router/relaykit/types"
	"new-router/service"
	"new-router/setting"
)

// PrepareRequestBilling estimates and reserves one request's charge. Transports
// provide the current request body through BodyStorage or BillingRequestInput;
// channel retries retain the resulting billing session and pricing snapshot.
func PrepareRequestBilling(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	channelSetting, _ := common.GetContextKeyType[dto.ChannelSettings](c, constant.ContextKeyChannelSetting)
	common.OptionMapRWMutex.RLock()
	globalSensitiveCheckEnabled := setting.ShouldCheckPromptSensitive()
	returnSensitiveWords := common.SensitiveErrorReturnWordsEnabled
	sensitiveCheckEnabled := channelSetting.ShouldCheckSensitive(globalSensitiveCheckEnabled)
	var globalSensitiveWords []string
	if sensitiveCheckEnabled && channelSetting.SensitiveMode != "override" {
		globalSensitiveWords = append(globalSensitiveWords, setting.SensitiveWords...)
	}
	common.OptionMapRWMutex.RUnlock()
	var effectiveSensitiveWords []string
	if sensitiveCheckEnabled {
		effectiveSensitiveWords = channelSetting.EffectiveSensitiveWords(globalSensitiveWords)
	}
	needSensitiveCheck := len(effectiveSensitiveWords) > 0
	meta := &types.TokenCountMeta{TokenType: types.TokenTypeTokenizer}
	if info.Request != nil && (needSensitiveCheck || constant.CountToken) {
		meta = info.Request.GetTokenCountMeta()
	} else {
		// Avoid building CombineText when only the pricing quantities are needed.
		switch request := info.Request.(type) {
		case *dto.GeneralOpenAIRequest:
			meta.MaxTokens = int(max(lo.FromPtr(request.MaxTokens), lo.FromPtr(request.MaxCompletionTokens)))
		case *dto.OpenAIResponsesRequest:
			meta.MaxTokens = int(lo.FromPtr(request.MaxOutputTokens))
		case *dto.ClaudeRequest:
			meta.MaxTokens = int(lo.FromPtr(request.MaxTokens))
		case *dto.ImageRequest:
			meta = request.GetTokenCountMeta()
		}
	}

	if needSensitiveCheck && meta != nil {
		if contains, words := service.CheckSensitiveTokenMetaWithWords(meta, effectiveSensitiveWords); contains {
			service.RequestPolicy(c).AddEvent(service.PolicyEvent{ErrorCode: string(types.ErrorCodeSensitiveWordsDetected), ErrorSource: "local", Decision: service.PolicyDecision{Action: "stop", Reason: "local_rejection", Source: "global"}, Health: "unchanged"})
			joinedWords := strings.Join(words, ", ")
			logger.LogWarn(c, fmt.Sprintf("user sensitive words detected: %s", joinedWords))
			sensitiveErr := errors.New("prompt contains sensitive words")
			errorOptions := []types.NewAPIErrorOptions{types.ErrOptionWithSkipRetry()}
			if returnSensitiveWords {
				sensitiveErr = fmt.Errorf("prompt contains sensitive words: %s", joinedWords)
				errorOptions = append(errorOptions, types.ErrOptionWithSensitiveWordsDisclosure())
			}
			return types.NewErrorWithStatusCode(sensitiveErr, types.ErrorCodeSensitiveWordsDetected, http.StatusBadRequest, errorOptions...)
		}
	}

	tokens, err := service.EstimateRequestToken(c, meta, info)
	if err != nil {
		return types.NewError(err, types.ErrorCodeCountTokenFailed)
	}
	info.SetEstimatePromptTokens(tokens)

	priceData, err := helper.ModelPriceHelper(c, info, tokens, meta)
	if err != nil {
		return types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithStatusCode(http.StatusBadRequest))
	}
	if priceData.FreeModel {
		logger.LogInfo(c, common.LogText("model %s is free, skipping pre-consume", info.OriginModelName))
		return nil
	}
	return service.PreConsumeBilling(c, priceData.QuotaToPreConsume, info)
}

// RefundFailedRequestBilling applies the common final-failure policy after all
// eligible attempts have ended. A settled BillingSession never refunds again.
func RefundFailedRequestBilling(c *gin.Context, info *relaycommon.RelayInfo, apiErr *types.NewAPIError) *types.NewAPIError {
	if apiErr == nil {
		return nil
	}
	apiErr = service.NormalizeViolationFeeError(apiErr)
	if info.Billing != nil {
		info.Billing.Refund(c)
	}
	service.ChargeViolationFeeIfNeeded(c, info, apiErr)
	return apiErr
}
