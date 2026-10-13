package helper

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	rootcommon "new-router/common"
	relaycommon "new-router/relay/common"
	relayconstant "new-router/relay/constant"
	"new-router/relaykit/dto"
	"new-router/setting/ratio_setting"
	hostreasoning "new-router/setting/reasoning"
)

func ModelMappedHelper(c *gin.Context, info *relaycommon.RelayInfo, request dto.Request) error {
	if info.ChannelMeta == nil {
		info.ChannelMeta = &relaycommon.ChannelMeta{}
	}

	// Preserve configured legacy compact aliases while keeping protocol modifiers
	// and ordinary model names on the upstream resolution path.
	mappingModelName := info.OriginModelName
	legacyCompact := info.RelayMode == relayconstant.RelayModeResponsesCompact && strings.HasSuffix(mappingModelName, ratio_setting.CompactModelSuffix)
	if legacyCompact {
		mappingModelName = strings.TrimSuffix(mappingModelName, ratio_setting.CompactModelSuffix)
		info.UpstreamModelName = mappingModelName
	}
	// map model name
	modelMapping := c.GetString("model_mapping")
	if modelMapping != "" && modelMapping != "{}" {
		modelMap := make(map[string]string)
		err := rootcommon.Unmarshal([]byte(modelMapping), &modelMap)
		if err != nil {
			return fmt.Errorf("unmarshal_model_mapping_failed")
		}

		// 支持链式模型重定向，最终使用链尾的模型
		currentModel := mappingModelName
		visitedModels := map[string]bool{
			currentModel: true,
		}
		for {
			mappedModel, exists := modelMap[currentModel]
			baseModel := hostreasoning.BaseModelName(currentModel)
			if (!exists || mappedModel == "") && baseModel != currentModel {
				mappedModel, exists = modelMap[baseModel]
			}
			if exists && mappedModel != "" {
				// 模型重定向循环检测，避免无限循环
				if visitedModels[mappedModel] {
					if mappedModel == currentModel {
						if currentModel == info.OriginModelName && !legacyCompact {
							info.IsModelMapped = false
							return nil
						}

						info.IsModelMapped = true
						break
					}
					return errors.New("model_mapping_contains_cycle")
				}
				visitedModels[mappedModel] = true
				currentModel = mappedModel
				info.IsModelMapped = true
			} else {
				break
			}
		}
		if info.IsModelMapped {
			info.UpstreamModelName = currentModel
		}
	}

	if legacyCompact {
		info.BillingModelName = ratio_setting.WithCompactModelSuffix(info.UpstreamModelName)
	}
	if request != nil {
		request.SetModelName(info.UpstreamModelName)
	}
	return nil
}
