package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"new-router/common"
	"new-router/constant"
	"new-router/i18n"
	"new-router/middleware"
	"new-router/model"
	"new-router/pkg/jsplugin"
	relaychannel "new-router/relay/channel"
	"new-router/relay/channel/ollama"
	relaycommon "new-router/relay/common"
	"new-router/relaykit/dto"
	"new-router/service"
	"new-router/service/authz"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type OpenAIModel struct {
	ID         string         `json:"id"`
	Object     string         `json:"object"`
	Created    int64          `json:"created"`
	OwnedBy    string         `json:"owned_by"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	Permission []struct {
		ID                 string `json:"id"`
		Object             string `json:"object"`
		Created            int64  `json:"created"`
		AllowCreateEngine  bool   `json:"allow_create_engine"`
		AllowSampling      bool   `json:"allow_sampling"`
		AllowLogprobs      bool   `json:"allow_logprobs"`
		AllowSearchIndices bool   `json:"allow_search_indices"`
		AllowView          bool   `json:"allow_view"`
		AllowFineTuning    bool   `json:"allow_fine_tuning"`
		Organization       string `json:"organization"`
		Group              string `json:"group"`
		IsBlocking         bool   `json:"is_blocking"`
	} `json:"permission"`
	Root   string `json:"root"`
	Parent string `json:"parent"`
}

type OpenAIModelsResponse struct {
	Data    []OpenAIModel `json:"data"`
	Success bool          `json:"success"`
}

func parseStatusFilter(statusParam string) int {
	switch strings.ToLower(statusParam) {
	case "enabled", "1":
		return common.ChannelStatusEnabled
	case "disabled", "0":
		return 0
	default:
		return -1
	}
}

func clearChannelInfo(channel *model.Channel) {
	if channel.ChannelInfo.IsMultiKey {
		channel.ChannelInfo.MultiKeyDisabledReason = nil
		channel.ChannelInfo.MultiKeyDisabledTime = nil
	}
}

func channelIDsFromChannels(channels []*model.Channel) []int {
	ids := make([]int, 0, len(channels))
	for _, channel := range channels {
		if channel != nil && channel.Id > 0 {
			ids = append(ids, channel.Id)
		}
	}
	return ids
}

func closeActiveChannelWebSockets(channelIDs []int) {
	service.CloseActiveWebSocketsForChannels(channelIDs, service.ChannelDisabledCloseReason)
}

func hasEnabledMultiKey(channel *model.Channel) bool {
	if channel == nil || !channel.ChannelInfo.IsMultiKey {
		return true
	}
	keys := channel.GetKeys()
	if len(keys) == 0 {
		return false
	}
	for i := range keys {
		if channel.ChannelInfo.MultiKeyStatusList == nil {
			return true
		}
		if status, ok := channel.ChannelInfo.MultiKeyStatusList[i]; !ok || status == common.ChannelStatusEnabled {
			return true
		}
	}
	return false
}

func disableMultiKeyChannelIfUnavailable(channel *model.Channel) bool {
	if channel == nil || !channel.ChannelInfo.IsMultiKey || hasEnabledMultiKey(channel) {
		return false
	}
	channel.NormalizeMultiKeyStatus()
	return true
}

func restoreMultiKeyChannelIfAvailable(channel *model.Channel) {
	if channel.Status != common.ChannelStatusManuallyDisabled || !hasEnabledMultiKey(channel) {
		return
	}
	info := channel.GetOtherInfo()
	if info["status_reason"] != model.ChannelStatusReasonAllKeysDisabled {
		return
	}
	channel.Status = common.ChannelStatusEnabled
	info["status_reason"] = ""
	info["status_time"] = common.GetTimestamp()
	channel.SetOtherInfo(info)
}

func applyChannelStatusFilter(query *gorm.DB, statusFilter int) *gorm.DB {
	if statusFilter == common.ChannelStatusEnabled {
		return query.Where("status = ?", common.ChannelStatusEnabled)
	}
	if statusFilter == 0 {
		return query.Where("status != ?", common.ChannelStatusEnabled)
	}
	return query
}

func buildChannelListQuery(group string, statusFilter int, typeFilter int, managedTags []string) *gorm.DB {
	query := model.DB.Model(&model.Channel{})
	query = model.ApplyChannelGroupFilter(query, group)
	query = applyChannelStatusFilter(query, statusFilter)
	if typeFilter >= 0 {
		query = query.Where("type = ?", typeFilter)
	}
	// 普通管理员的标签白名单裁剪:仅返回其可管理标签下的渠道。白名单为空表示不限。
	if len(managedTags) > 0 {
		query = query.Where("tag IN ?", managedTags)
	}
	return query
}

func GetChannelOps(c *gin.Context) {
	snapshot := model.CurrentRequestPolicy()
	automaticDisable, source := snapshot.AutoDisable, "global"
	if value, present := c.GetQuery("auto_ban"); present {
		autoBan, err := strconv.ParseBool(value)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		automaticDisable = snapshot.AutoDisable && autoBan
		if snapshot.AutoDisable {
			source = "global_and_channel"
			if !autoBan {
				source = "channel"
			}
		}
	}
	common.ApiSuccess(c, gin.H{
		"retry_times":    snapshot.RetryTimes,
		"request_policy": gin.H{"automatic_disable": automaticDisable, "source": source},
	})
}

func GetChannelDefaultBaseURLs(c *gin.Context) {
	baseURLs := make(map[int]string)
	for channelType, baseURL := range constant.ChannelBaseURLs {
		if baseURL != "" {
			baseURLs[channelType] = baseURL
		}
	}
	common.ApiSuccess(c, baseURLs)
}

func GetAllChannels(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	channelData := make([]*model.Channel, 0)
	idSort, _ := strconv.ParseBool(c.Query("id_sort"))
	sortOptions := model.NewChannelSortOptions(c.Query("sort_by"), c.Query("sort_order"), idSort)
	enableTagMode, _ := strconv.ParseBool(c.Query("tag_mode"))
	groupFilter := model.NormalizeChannelGroupFilter(c.Query("group"))
	statusParam := c.Query("status")
	// statusFilter: -1 all, 1 enabled, 0 disabled (include auto & manual)
	statusFilter := parseStatusFilter(statusParam)
	// type filter
	typeStr := c.Query("type")
	typeFilter := -1
	if typeStr != "" {
		if t, err := strconv.Atoi(typeStr); err == nil {
			typeFilter = t
		}
	}

	managedTags := getAdminManagedTags(c)

	var total int64

	if enableTagMode {
		tags, err := model.GetPaginatedChannelTags(buildChannelListQuery(groupFilter, statusFilter, typeFilter, managedTags), pageInfo.GetStartIdx(), pageInfo.GetPageSize())
		if err != nil {
			common.SysError(common.LogText("failed to get paginated tags: %s", err.Error()))
			common.ApiErrorT(c, "Failed to fetch tags. Please try again later")
			return
		}
		total, err = model.CountChannelTags(buildChannelListQuery(groupFilter, statusFilter, typeFilter, managedTags))
		if err != nil {
			common.SysError(common.LogText("failed to count tags: %s", err.Error()))
			common.ApiErrorT(c, "Failed to count tags. Please try again later")
			return
		}
		for _, tag := range tags {
			if tag == nil || *tag == "" {
				continue
			}
			var tagChannels []*model.Channel
			err := sortOptions.Apply(buildChannelListQuery(groupFilter, statusFilter, typeFilter, managedTags).Where("tag = ?", *tag)).
				Omit("key").
				Find(&tagChannels).Error
			if err != nil {
				common.SysError(common.LogText("failed to get channels by tag: %s", err.Error()))
				common.ApiErrorT(c, "Failed to fetch channels with this tag. Please try again later")
				return
			}
			channelData = append(channelData, tagChannels...)
		}
	} else {
		if err := buildChannelListQuery(groupFilter, statusFilter, typeFilter, managedTags).Count(&total).Error; err != nil {
			common.SysError(common.LogText("failed to count channels: %s", err.Error()))
			common.ApiErrorT(c, "Failed to count channels. Please try again later")
			return
		}

		err := sortOptions.Apply(buildChannelListQuery(groupFilter, statusFilter, typeFilter, managedTags)).
			Limit(pageInfo.GetPageSize()).
			Offset(pageInfo.GetStartIdx()).
			Omit("key").
			Find(&channelData).Error
		if err != nil {
			common.SysError(common.LogText("failed to get channels: %s", err.Error()))
			common.ApiErrorT(c, "Failed to fetch channels. Please try again later")
			return
		}
	}

	for _, datum := range channelData {
		clearChannelInfo(datum)
	}

	countQuery := buildChannelListQuery(groupFilter, statusFilter, -1, managedTags)
	var results []struct {
		Type  int64
		Count int64
	}
	if err := countQuery.Select("type, count(*) as count").Group("type").Find(&results).Error; err != nil {
		common.SysError(common.LogText("failed to count channel types: %s", err.Error()))
		common.ApiErrorT(c, "Failed to count channel types. Please try again later")
		return
	}
	typeCounts := make(map[int64]int64)
	for _, r := range results {
		typeCounts[r.Type] = r.Count
	}
	common.ApiSuccess(c, gin.H{
		"items":       channelData,
		"total":       total,
		"page":        pageInfo.GetPage(),
		"page_size":   pageInfo.GetPageSize(),
		"type_counts": typeCounts,
	})
	return
}

func buildFetchModelsHeaders(channel *model.Channel, key string) (http.Header, error) {
	var headers http.Header
	switch channel.Type {
	case constant.ChannelTypeAnthropic:
		headers = GetClaudeAuthHeader(key)
	default:
		headers = GetAuthHeader(key)
	}

	if err := applyFetchModelsHeaderOverrides(channel, key, headers); err != nil {
		return nil, err
	}
	return headers, nil
}

func applyFetchModelsHeaderOverrides(channel *model.Channel, key string, headers http.Header) error {
	info := &relaycommon.RelayInfo{
		IsChannelTest: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey:          key,
			HeadersOverride: channel.GetHeaderOverride(),
		},
	}
	overrides, err := relaychannel.ResolveHeaderOverride(info, nil, headers)
	if err != nil {
		return err
	}
	for name, value := range overrides {
		headers.Set(name, value)
	}

	return nil
}

func FetchUpstreamModels(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}

	channel, err := model.GetChannelById(id, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	ids, err := fetchChannelUpstreamModelIDs(channel)
	if err != nil {
		common.ApiErrorT(c, "Failed to fetch models: {{error}}", map[string]any{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    ids,
	})
}

func FixChannelsAbilities(c *gin.Context) {
	if !assertNotTagRestricted(c) {
		return
	}
	success, fails, err := model.FixAbility()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"success": success,
			"fails":   fails,
		},
	})
}

func SearchChannels(c *gin.Context) {
	keyword := c.Query("keyword")
	group := c.Query("group")
	managedTags := getAdminManagedTags(c)
	modelKeyword := c.Query("model")
	statusParam := c.Query("status")
	statusFilter := parseStatusFilter(statusParam)
	idSort, _ := strconv.ParseBool(c.Query("id_sort"))
	sortOptions := model.NewChannelSortOptions(c.Query("sort_by"), c.Query("sort_order"), idSort)
	enableTagMode, _ := strconv.ParseBool(c.Query("tag_mode"))
	channelData := make([]*model.Channel, 0)
	if enableTagMode {
		tags, err := model.SearchTags(keyword, group, modelKeyword, idSort)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		for _, tag := range tags {
			if tag != nil && *tag != "" {
				var tagChannels []*model.Channel
				err := sortOptions.Apply(buildChannelListQuery(group, -1, -1, managedTags).Where("tag = ?", *tag)).
					Omit("key").
					Find(&tagChannels).Error
				if err != nil {
					common.ApiError(c, err)
					return
				}
				channelData = append(channelData, tagChannels...)
			}
		}
	} else {
		channels, err := model.SearchChannels(keyword, group, modelKeyword, idSort, sortOptions)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		channelData = channels
	}

	// 非 tag 模式经 model.SearchChannels 未走 buildChannelListQuery,这里统一按白名单裁剪,
	// 保证两条路径下受限管理员都只看到可管理标签的渠道。白名单为空则放行全部。
	if len(managedTags) > 0 {
		filteredByTag := make([]*model.Channel, 0, len(channelData))
		for _, ch := range channelData {
			if tagInManagedSet(managedTags, ch.GetTag()) {
				filteredByTag = append(filteredByTag, ch)
			}
		}
		channelData = filteredByTag
	}

	if statusFilter == common.ChannelStatusEnabled || statusFilter == 0 {
		filtered := make([]*model.Channel, 0, len(channelData))
		for _, ch := range channelData {
			if statusFilter == common.ChannelStatusEnabled && ch.Status != common.ChannelStatusEnabled {
				continue
			}
			if statusFilter == 0 && ch.Status == common.ChannelStatusEnabled {
				continue
			}
			filtered = append(filtered, ch)
		}
		channelData = filtered
	}

	// calculate type counts for search results
	typeCounts := make(map[int64]int64)
	for _, channel := range channelData {
		typeCounts[int64(channel.Type)]++
	}

	typeParam := c.Query("type")
	typeFilter := -1
	if typeParam != "" {
		if tp, err := strconv.Atoi(typeParam); err == nil {
			typeFilter = tp
		}
	}

	if typeFilter >= 0 {
		filtered := make([]*model.Channel, 0, len(channelData))
		for _, ch := range channelData {
			if ch.Type == typeFilter {
				filtered = append(filtered, ch)
			}
		}
		channelData = filtered
	}

	page, _ := strconv.Atoi(c.DefaultQuery("p", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}

	total := len(channelData)
	startIdx := min((page-1)*pageSize, total)
	endIdx := min(startIdx+pageSize, total)

	pagedData := channelData[startIdx:endIdx]

	for _, datum := range pagedData {
		clearChannelInfo(datum)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"items":       pagedData,
			"total":       total,
			"type_counts": typeCounts,
		},
	})
	return
}

func GetChannel(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	channel, err := model.GetChannelById(id, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !assertChannelTagAllowed(c, channel) {
		return
	}
	if channel != nil {
		clearChannelInfo(channel)
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    channel,
	})
	return
}

// GetChannelKey 获取渠道密钥（需要通过安全验证中间件）
// 此函数依赖 SecureVerificationRequired 中间件，确保用户已通过安全验证
func GetChannelKey(c *gin.Context) {
	channelId, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelId <= 0 {
		common.ApiErrorT(c, "Invalid channel ID")
		return
	}

	// 获取渠道信息（包含密钥）
	channel, err := model.GetChannelById(channelId, true)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		common.ApiErrorT(c, "Channel does not exist")
		return
	}
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}

	if !assertChannelTagAllowed(c, channel) {
		return
	}

	// 记录操作审计日志（高危：查看渠道密钥）
	recordManageAudit(c, "channel.key_view", map[string]any{
		"id":   channelId,
		"name": channel.Name,
	})

	// 返回渠道密钥
	common.ApiSuccessT(c, "Fetched successfully", map[string]any{
		"key": channel.Key,
	})
}

// ExportChannelsRequest 限定导出范围；ids 为空表示导出全部渠道。
type ExportChannelsRequest struct {
	Ids []int `json:"ids"`
}

// channelExportInfo 仅保留导入时重建多 Key 渠道所需的字段，
// key 状态列表等运行时数据不导出。
type channelExportInfo struct {
	IsMultiKey   bool                  `json:"is_multi_key,omitempty"`
	MultiKeyMode constant.MultiKeyMode `json:"multi_key_mode,omitempty"`
}

// channelExportItem 是导出文件中的单个渠道，仅包含可迁移的配置字段，
// 余额、用量、测试结果等运行时统计不参与导出。
type channelExportItem struct {
	Type               int                `json:"type"`
	Name               string             `json:"name"`
	Key                string             `json:"key"`
	BaseURL            *string            `json:"base_url,omitempty"`
	Other              string             `json:"other,omitempty"`
	Models             string             `json:"models"`
	Group              string             `json:"group"`
	ModelMapping       *string            `json:"model_mapping,omitempty"`
	StatusCodeMapping  *string            `json:"status_code_mapping,omitempty"`
	Priority           *int64             `json:"priority,omitempty"`
	Weight             *uint              `json:"weight,omitempty"`
	AutoBan            *int               `json:"auto_ban,omitempty"`
	TestModel          *string            `json:"test_model,omitempty"`
	OpenAIOrganization *string            `json:"openai_organization,omitempty"`
	Tag                *string            `json:"tag,omitempty"`
	Setting            *string            `json:"setting,omitempty"`
	ParamOverride      *string            `json:"param_override,omitempty"`
	HeaderOverride     *string            `json:"header_override,omitempty"`
	Remark             *string            `json:"remark,omitempty"`
	Status             int                `json:"status"`
	ChannelInfo        *channelExportInfo `json:"channel_info,omitempty"`
	OtherSettings      string             `json:"settings,omitempty"`
}

func newChannelExportItem(channel *model.Channel) channelExportItem {
	item := channelExportItem{
		Type:               channel.Type,
		Name:               channel.Name,
		Key:                channel.Key,
		BaseURL:            channel.BaseURL,
		Other:              channel.Other,
		Models:             channel.Models,
		Group:              channel.Group,
		ModelMapping:       channel.ModelMapping,
		StatusCodeMapping:  channel.StatusCodeMapping,
		Priority:           channel.Priority,
		Weight:             channel.Weight,
		AutoBan:            channel.AutoBan,
		TestModel:          channel.TestModel,
		OpenAIOrganization: channel.OpenAIOrganization,
		Tag:                channel.Tag,
		Setting:            channel.Setting,
		ParamOverride:      channel.ParamOverride,
		HeaderOverride:     channel.HeaderOverride,
		Remark:             channel.Remark,
		Status:             channel.Status,
		OtherSettings:      channel.OtherSettings,
	}
	if channel.ChannelInfo.IsMultiKey {
		item.ChannelInfo = &channelExportInfo{
			IsMultiKey:   true,
			MultiKeyMode: channel.ChannelInfo.MultiKeyMode,
		}
	}
	return item
}

// ExportChannels 批量导出渠道配置（含明文密钥）。
// The root-only route consumes a proof bound to the selected channel IDs below.
func ExportChannels(c *gin.Context) {
	req := ExportChannelsRequest{}
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	context, err := common.Marshal(service.ChannelKeyExportContext{ChannelIDs: append([]int{}, req.Ids...)})
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	if middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeChannelKeyExport, Context: context}) == nil {
		return
	}

	var channels []*model.Channel
	if len(req.Ids) > 0 {
		channels, err = model.GetChannelsByIds(req.Ids)
	} else {
		channels, err = model.GetAllChannels(0, 0, true, true)
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}

	items := make([]channelExportItem, 0, len(channels))
	for _, channel := range channels {
		if channel == nil {
			continue
		}
		items = append(items, newChannelExportItem(channel))
	}

	// 记录操作审计日志（高危：批量导出含密钥的渠道配置）
	recordManageAudit(c, "channel.export", map[string]interface{}{
		"count": len(items),
	})

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"version":  1,
			"channels": items,
		},
	})
}

// validateTwoFactorAuth 统一的2FA验证函数
func validateTwoFactorAuth(twoFA *model.TwoFA, code string) bool {
	// 尝试验证TOTP
	if cleanCode, err := common.ValidateNumericCode(code); err == nil {
		if isValid, _ := twoFA.ValidateTOTPAndUpdateUsage(cleanCode); isValid {
			return true
		}
	}

	// 尝试验证备用码
	if isValid, err := twoFA.ValidateBackupCodeAndUpdateUsage(code); err == nil && isValid {
		return true
	}

	return false
}

// maxTaskExtendPluginKeys bounds the plugins one New API channel can be
// extended with; it comfortably covers every built-in and installed plugin.
const maxTaskExtendPluginKeys = 32

// validateChannel 通用的渠道校验函数
func validateChannel(channel *model.Channel, isAdd bool) error {
	if channel == nil {
		return fmt.Errorf("channel cannot be empty")
	}

	// 校验 channel settings
	if err := channel.ValidateSettings(); err != nil {
		return common.NewMessage("Invalid channel settings format: {{error}}", map[string]any{"error": err.Error()})
	}
	if channel.Type == constant.ChannelTypeTaskPlugin {
		pluginKey := strings.TrimSpace(channel.GetSetting().TaskPluginKey)
		if pluginKey == "" {
			return fmt.Errorf("task plugin key is required")
		}
		if len(pluginKey) > 30 {
			return fmt.Errorf("task plugin key must not exceed 30 characters")
		}
		plugin, ok := jsplugin.DefaultRegistry.Get(pluginKey)
		if !ok {
			return fmt.Errorf("task plugin %q is not registered", pluginKey)
		}
		if channel.BaseURL == nil || strings.TrimSpace(*channel.BaseURL) == "" {
			// The plugin default is persisted onto the channel instead of being
			// resolved per request, so the destination host stays an auditable
			// channel property that only an administrator edit can change.
			if plugin.Meta.BaseURL == "" {
				return fmt.Errorf("base URL is required for task plugin channels")
			}
			defaultBaseURL := plugin.Meta.BaseURL
			channel.BaseURL = &defaultBaseURL
		}
	}

	setting := channel.GetSetting()
	if channel.Type != constant.ChannelTypeNewAPI && len(setting.TaskExtendPluginKeys) > 0 {
		return fmt.Errorf("task_extend_plugin_keys is only supported on New API channels")
	}
	if channel.Type == constant.ChannelTypeNewAPI {
		if len(setting.TaskExtendPluginKeys) > maxTaskExtendPluginKeys {
			return fmt.Errorf("task_extend_plugin_keys must not exceed %d plugins", maxTaskExtendPluginKeys)
		}
		// The single key stays valid on a New API channel, so both bindings are
		// checked as one set.
		keys := setting.TaskExtendPluginKeys
		if setting.TaskPluginKey != "" {
			keys = append([]string{setting.TaskPluginKey}, keys...)
		}
		bound := make(map[string]struct{}, len(keys))
		for _, key := range keys {
			if key == "" || key != strings.TrimSpace(key) {
				return fmt.Errorf("task plugin key %q is invalid", key)
			}
			if len(key) > 30 {
				return fmt.Errorf("task plugin key must not exceed 30 characters")
			}
			if _, duplicate := bound[key]; duplicate {
				return fmt.Errorf("task plugin %q is bound more than once", key)
			}
			plugin, ok := jsplugin.DefaultRegistry.Get(key)
			if !ok {
				return fmt.Errorf("task plugin %q is not registered", key)
			}
			if !plugin.Meta.SupportsUpstream(jsplugin.UpstreamKindNewAPI) {
				return fmt.Errorf("task plugin %q does not support a New API upstream and cannot be bound to a New API channel", key)
			}
			bound[key] = struct{}{}
		}
	}

	if channel.Type == constant.ChannelTypeNewAPI && strings.TrimSpace(channel.GetBaseURL()) == "" {
		return fmt.Errorf("New API channel base URL cannot be empty")
	}
	if channel.Type == constant.ChannelTypeVLLM && strings.TrimSpace(channel.GetBaseURL()) == "" {
		return fmt.Errorf("vLLM channel base URL cannot be empty")
	}
	if channel.Type == constant.ChannelTypeSGLang && strings.TrimSpace(channel.GetBaseURL()) == "" {
		return fmt.Errorf("SGLang channel base URL cannot be empty")
	}

	// 如果是添加操作，检查 channel 和 key 是否为空
	if isAdd {
		if channel.Key == "" {
			return fmt.Errorf("channel cannot be empty")
		}

		// 检查模型名称长度是否超过 255
		for _, m := range channel.GetModels() {
			if len(m) > 255 {
				return common.NewMessage("Model name is too long: {{model}}", map[string]any{"model": m})
			}
		}
	}

	// VertexAI 特殊校验
	if channel.Type == constant.ChannelTypeVertexAi {
		if channel.Other == "" {
			return common.NewMessage("Deployment region cannot be empty")
		}

		regionMap, err := common.StrToMap(channel.Other)
		if err != nil {
			return common.NewMessage("Deployment region must be valid JSON, for example {{example}}", map[string]any{"example": `{"default": "us-central1", "region2": "us-east1"}`})
		}

		if regionMap["default"] == nil {
			return common.NewMessage("Deployment region must include a default field")
		}
	}

	// Codex OAuth key validation (optional, only when JSON object is provided)
	if channel.Type == constant.ChannelTypeCodex {
		trimmedKey := strings.TrimSpace(channel.Key)
		if isAdd || trimmedKey != "" {
			if !strings.HasPrefix(trimmedKey, "{") {
				return fmt.Errorf("Codex key must be a valid JSON object")
			}
			var keyMap map[string]any
			if err := common.Unmarshal([]byte(trimmedKey), &keyMap); err != nil {
				return fmt.Errorf("Codex key must be a valid JSON object")
			}
			if v, ok := keyMap["access_token"]; !ok || v == nil || strings.TrimSpace(fmt.Sprintf("%v", v)) == "" {
				return fmt.Errorf("Codex key JSON must include access_token")
			}
			if v, ok := keyMap["account_id"]; !ok || v == nil || strings.TrimSpace(fmt.Sprintf("%v", v)) == "" {
				return fmt.Errorf("Codex key JSON must include account_id")
			}
		}
	}

	return nil
}

func RefreshCodexChannelCredential(c *gin.Context) {
	channelId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, fmt.Errorf("invalid channel id: %w", err))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	oauthKey, ch, err := service.RefreshCodexChannelCredential(ctx, channelId, service.CodexCredentialRefreshOptions{ResetCaches: true})
	if err != nil {
		common.SysError(common.LogText("failed to refresh codex channel credential: %s", err.Error()))
		common.ApiErrorT(c, "Failed to refresh credentials. Please try again later")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "refreshed",
		"data": gin.H{
			"expires_at":   oauthKey.Expired,
			"last_refresh": oauthKey.LastRefresh,
			"account_id":   oauthKey.AccountID,
			"email":        oauthKey.Email,
			"channel_id":   ch.Id,
			"channel_type": ch.Type,
			"channel_name": ch.Name,
		},
	})
}

type AddChannelRequest struct {
	Mode                      string                `json:"mode"`
	MultiKeyMode              constant.MultiKeyMode `json:"multi_key_mode"`
	BatchAddSetKeyPrefix2Name bool                  `json:"batch_add_set_key_prefix_2_name"`
	Channel                   *model.Channel        `json:"channel"`
}

func getVertexArrayKeys(keys string) ([]string, error) {
	if keys == "" {
		return nil, nil
	}
	var keyArray []any
	err := common.Unmarshal([]byte(keys), &keyArray)
	if err != nil {
		return nil, common.NewMessage("Batch-adding Vertex AI keys requires a JSON array, for example {{example}}. Check the input: {{error}}", map[string]any{"example": "[{key1}, {key2}...]", "error": err.Error()})
	}
	cleanKeys := make([]string, 0, len(keyArray))
	for _, key := range keyArray {
		var keyStr string
		switch v := key.(type) {
		case string:
			keyStr = strings.TrimSpace(v)
		default:
			bytes, err := common.Marshal(v)
			if err != nil {
				return nil, common.NewMessage("Failed to encode Vertex AI key JSON: {{error}}", map[string]any{"error": err.Error()})
			}
			keyStr = string(bytes)
		}
		if keyStr != "" {
			cleanKeys = append(cleanKeys, keyStr)
		}
	}
	if len(cleanKeys) == 0 {
		return nil, common.NewMessage("Vertex AI keys for batch adding cannot be empty")
	}
	return cleanKeys, nil
}

func AddChannel(c *gin.Context) {
	addChannelRequest := AddChannelRequest{}
	err := c.ShouldBindJSON(&addChannelRequest)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// Binding a plugin needs the same permission on a New API channel as the
	// type-61 channel dedicated to it.
	if addChannelRequest.Channel != nil &&
		(addChannelRequest.Channel.Type == constant.ChannelTypeTaskPlugin || len(addChannelRequest.Channel.GetSetting().TaskPluginBindings()) > 0) &&
		!middleware.PermissionAllowed(c, authz.TaskPluginBind) {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "task plugin channels require the task_plugin.bind permission",
		})
		return
	}

	baseURLFromPluginDefault := addChannelRequest.Channel != nil &&
		addChannelRequest.Channel.Type == constant.ChannelTypeTaskPlugin &&
		(addChannelRequest.Channel.BaseURL == nil || strings.TrimSpace(*addChannelRequest.Channel.BaseURL) == "")
	// 使用统一的校验函数
	if err := validateChannel(addChannelRequest.Channel, true); err != nil {
		common.ApiError(c, err)
		return
	}

	if !assertTagManageable(c, addChannelRequest.Channel.GetTag()) {
		return
	}

	addChannelRequest.Channel.CreatedTime = common.GetTimestamp()
	keys := make([]string, 0)
	switch addChannelRequest.Mode {
	case "multi_to_single":
		addChannelRequest.Channel.ChannelInfo.IsMultiKey = true
		addChannelRequest.Channel.ChannelInfo.MultiKeyMode = addChannelRequest.MultiKeyMode
		if addChannelRequest.Channel.Type == constant.ChannelTypeVertexAi && addChannelRequest.Channel.GetOtherSettings().VertexKeyType != dto.VertexKeyTypeAPIKey {
			array, err := getVertexArrayKeys(addChannelRequest.Channel.Key)
			if err != nil {
				common.ApiError(c, err)
				return
			}
			addChannelRequest.Channel.ChannelInfo.MultiKeySize = len(array)
			addChannelRequest.Channel.Key = strings.Join(array, "\n")
		} else {
			cleanKeys := make([]string, 0)
			for key := range strings.SplitSeq(addChannelRequest.Channel.Key, "\n") {
				if key == "" {
					continue
				}
				key = strings.TrimSpace(key)
				cleanKeys = append(cleanKeys, key)
			}
			addChannelRequest.Channel.ChannelInfo.MultiKeySize = len(cleanKeys)
			addChannelRequest.Channel.Key = strings.Join(cleanKeys, "\n")
		}
		keys = []string{addChannelRequest.Channel.Key}
	case "batch":
		if addChannelRequest.Channel.Type == constant.ChannelTypeVertexAi && addChannelRequest.Channel.GetOtherSettings().VertexKeyType != dto.VertexKeyTypeAPIKey {
			// multi json
			keys, err = getVertexArrayKeys(addChannelRequest.Channel.Key)
			if err != nil {
				common.ApiError(c, err)
				return
			}
		} else {
			keys = strings.Split(addChannelRequest.Channel.Key, "\n")
		}
	case "single":
		keys = []string{addChannelRequest.Channel.Key}
	default:
		common.ApiErrorT(c, "Unsupported add mode")
		return
	}

	channels := make([]model.Channel, 0, len(keys))
	for _, key := range keys {
		if key == "" {
			continue
		}
		localChannel := addChannelRequest.Channel
		localChannel.Key = key
		if addChannelRequest.BatchAddSetKeyPrefix2Name && len(keys) > 1 {
			keyPrefix := localChannel.Key
			if len(localChannel.Key) > 8 {
				keyPrefix = localChannel.Key[:8]
			}
			localChannel.Name = fmt.Sprintf("%s %s", localChannel.Name, keyPrefix)
		}
		channels = append(channels, *localChannel)
	}
	err = model.BatchInsertChannels(channels)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	createAudit := map[string]any{
		"name":  addChannelRequest.Channel.Name,
		"type":  addChannelRequest.Channel.Type,
		"count": len(channels),
	}
	if baseURLFromPluginDefault {
		createAudit["base_url_source"] = "plugin_default"
	}
	recordManageAudit(c, "channel.create", createAudit)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func DeleteChannel(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if !assertChannelIDManageable(c, id) {
		return
	}
	channelName := ""
	channelProxy := ""
	channelLookupFailed := false
	if existing, err := model.GetChannelById(id, false); err == nil && existing != nil {
		channelName = existing.Name
		channelProxy = existing.GetSetting().Proxy
	} else {
		channelLookupFailed = true
	}
	channel := model.Channel{Id: id}
	err := channel.Delete()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	if channelLookupFailed {
		service.ResetProxyClientCache()
	} else {
		service.InvalidateProxyClient(channelProxy)
	}
	recordManageAudit(c, "channel.delete", map[string]any{
		"id":   id,
		"name": channelName,
	})
	closeActiveChannelWebSockets([]int{id})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func DeleteDisabledChannel(c *gin.Context) {
	if !assertNotTagRestricted(c) {
		return
	}
	var ids []int
	if err := model.DB.Model(&model.Channel{}).
		Where("status = ? or status = ?", common.ChannelStatusAutoDisabled, common.ChannelStatusManuallyDisabled).
		Pluck("id", &ids).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	rows, err := model.DeleteDisabledChannel()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	if rows > 0 {
		service.ResetProxyClientCache()
	}
	recordManageAudit(c, "channel.delete_disabled", map[string]any{
		"count": rows,
	})
	closeActiveChannelWebSockets(ids)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    rows,
	})
	return
}

type ChannelTag struct {
	Tag            string  `json:"tag"`
	NewTag         *string `json:"new_tag"`
	Priority       *int64  `json:"priority"`
	Weight         *uint   `json:"weight"`
	ModelMapping   *string `json:"model_mapping"`
	Models         *string `json:"models"`
	Groups         *string `json:"groups"`
	ParamOverride  *string `json:"param_override"`
	HeaderOverride *string `json:"header_override"`
}

func DisableTagChannels(c *gin.Context) {
	channelTag := ChannelTag{}
	err := c.ShouldBindJSON(&channelTag)
	if err != nil || channelTag.Tag == "" {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if !assertTagManageable(c, channelTag.Tag) {
		return
	}
	channels, err := model.GetChannelsByTag(channelTag.Tag, false, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	ids := channelIDsFromChannels(channels)
	err = model.DisableChannelByTag(channelTag.Tag)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	recordManageAudit(c, "channel.tag_disable", map[string]any{
		"tag": channelTag.Tag,
	})
	closeActiveChannelWebSockets(ids)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func EnableTagChannels(c *gin.Context) {
	channelTag := ChannelTag{}
	err := c.ShouldBindJSON(&channelTag)
	if err != nil || channelTag.Tag == "" {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if !assertTagManageable(c, channelTag.Tag) {
		return
	}
	err = model.EnableChannelByTag(channelTag.Tag)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	recordManageAudit(c, "channel.tag_enable", map[string]any{
		"tag": channelTag.Tag,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func EditTagChannels(c *gin.Context) {
	channelTag := ChannelTag{}
	err := c.ShouldBindJSON(&channelTag)
	if err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if channelTag.Tag == "" {
		common.ApiErrorT(c, "Tag cannot be empty")
		return
	}
	if !assertTagManageable(c, channelTag.Tag) {
		return
	}
	if (channelTag.ParamOverride != nil || channelTag.HeaderOverride != nil) &&
		!middleware.PermissionAllowed(c, authz.ChannelSensitiveWrite) {
		common.ApiErrorT(c, "Unauthorized, insufficient privileges")
		return
	}
	if channelTag.ParamOverride != nil {
		trimmed := strings.TrimSpace(*channelTag.ParamOverride)
		if trimmed != "" && !json.Valid([]byte(trimmed)) {
			common.ApiErrorT(c, "Parameter override must be valid JSON format")
			return
		}
		channelTag.ParamOverride = common.GetPointer[string](trimmed)
	}
	if channelTag.HeaderOverride != nil {
		trimmed := strings.TrimSpace(*channelTag.HeaderOverride)
		if trimmed != "" && !json.Valid([]byte(trimmed)) {
			common.ApiErrorT(c, "Header override must be valid JSON format")
			return
		}
		channelTag.HeaderOverride = common.GetPointer[string](trimmed)
	}
	err = model.EditChannelByTag(channelTag.Tag, channelTag.NewTag, channelTag.ModelMapping, channelTag.Models, channelTag.Groups, channelTag.Priority, channelTag.Weight, channelTag.ParamOverride, channelTag.HeaderOverride)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	recordManageAudit(c, "channel.tag_edit", map[string]any{
		"tag": channelTag.Tag,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

type ChannelBatch struct {
	Ids []int   `json:"ids"`
	Tag *string `json:"tag"`
}

func DeleteChannelBatch(c *gin.Context) {
	channelBatch := ChannelBatch{}
	err := c.ShouldBindJSON(&channelBatch)
	if err != nil || len(channelBatch.Ids) == 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if !assertChannelIDsManageable(c, channelBatch.Ids) {
		return
	}
	deletedCount, err := model.BatchDeleteChannels(channelBatch.Ids)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	if deletedCount > 0 {
		service.ResetProxyClientCache()
	}
	recordManageAudit(c, "channel.delete_batch", map[string]any{
		"count": deletedCount,
	})
	closeActiveChannelWebSockets(channelBatch.Ids)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    deletedCount,
	})
	return
}

type PatchChannel struct {
	model.Channel
	MultiKeyMode *string `json:"multi_key_mode"`
	KeyMode      *string `json:"key_mode"` // 多key模式下密钥覆盖或者追加
}

type ChannelStatusRequest struct {
	Status int `json:"status"`
}

type ChannelStatusBatchRequest struct {
	Ids    []int `json:"ids"`
	Status int   `json:"status"`
}

func UpdateChannel(c *gin.Context) {
	channel := PatchChannel{}
	rawBody, err := c.GetRawData()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := common.Unmarshal(rawBody, &channel); err != nil {
		common.ApiError(c, err)
		return
	}
	var requestData map[string]any
	if err := common.Unmarshal(rawBody, &requestData); err != nil {
		common.ApiError(c, err)
		return
	}
	if _, ok := requestData["status"]; ok {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	clearChannelReadOnlyFields(&channel, requestData)

	if channel.Type == constant.ChannelTypeTaskPlugin &&
		!middleware.PermissionAllowed(c, authz.TaskPluginBind) {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "task plugin channels require the task_plugin.bind permission",
		})
		return
	}

	baseURLFromPluginDefault := channel.Type == constant.ChannelTypeTaskPlugin &&
		(channel.BaseURL == nil || strings.TrimSpace(*channel.BaseURL) == "")
	// 使用统一的校验函数
	if err := validateChannel(&channel.Channel, false); err != nil {
		common.ApiError(c, err)
		return
	}
	// Preserve existing ChannelInfo to ensure multi-key channels keep correct state even if the client does not send ChannelInfo in the request.
	originChannel, err := model.GetChannelById(channel.Id, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	originProxy := originChannel.GetSetting().Proxy
	proxyChanged := false
	_, settingProvided := requestData["setting"]
	if settingProvided {
		newProxy, _ := service.NormalizeProxyURL(channel.GetSetting().Proxy)
		normalizedOriginProxy, originProxyErr := service.NormalizeProxyURL(originProxy)
		proxyChanged = originProxyErr != nil || normalizedOriginProxy != newProxy
	}
	// Changing which plugins a channel binds needs the bind permission on any
	// channel type; resubmitting an unchanged New API binding list does not, so
	// administrators without it can still edit the rest of a gateway channel.
	if settingProvided &&
		!slices.Equal(channel.GetSetting().TaskPluginBindings(), originChannel.GetSetting().TaskPluginBindings()) &&
		!middleware.PermissionAllowed(c, authz.TaskPluginBind) {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "task plugin channels require the task_plugin.bind permission",
		})
		return
	}

	if !assertChannelTagAllowed(c, originChannel) {
		return
	}
	if _, tagChanged := requestData["tag"]; tagChanged && channel.GetTag() != originChannel.GetTag() {
		if !assertTagManageable(c, channel.GetTag()) {
			return
		}
	}

	// Always copy the original ChannelInfo so that fields like IsMultiKey and MultiKeySize are retained.
	channel.ChannelInfo = originChannel.ChannelInfo

	if channelHasSensitiveChanges(&channel, originChannel, requestData) &&
		!middleware.PermissionAllowed(c, authz.ChannelSensitiveWrite) {
		common.ApiErrorT(c, "Unauthorized, insufficient privileges")
		return
	}

	// If the request explicitly specifies a new MultiKeyMode, apply it on top of the original info.
	if channel.MultiKeyMode != nil && *channel.MultiKeyMode != "" {
		channel.ChannelInfo.MultiKeyMode = constant.MultiKeyMode(*channel.MultiKeyMode)
	}

	// 处理多key模式下的密钥追加/覆盖逻辑
	if channel.KeyMode != nil && channel.ChannelInfo.IsMultiKey {
		switch *channel.KeyMode {
		case "append":
			// 追加模式：将新密钥添加到现有密钥列表
			if originChannel.Key != "" {
				var newKeys []string
				var existingKeys []string

				// 解析现有密钥
				if strings.HasPrefix(strings.TrimSpace(originChannel.Key), "[") {
					// JSON数组格式
					var arr []json.RawMessage
					if err := json.Unmarshal([]byte(strings.TrimSpace(originChannel.Key)), &arr); err == nil {
						existingKeys = make([]string, len(arr))
						for i, v := range arr {
							existingKeys[i] = string(v)
						}
					}
				} else {
					// 换行分隔格式
					existingKeys = strings.Split(strings.Trim(originChannel.Key, "\n"), "\n")
				}

				// 处理 Vertex AI 的特殊情况
				if channel.Type == constant.ChannelTypeVertexAi && channel.GetOtherSettings().VertexKeyType != dto.VertexKeyTypeAPIKey {
					// 尝试解析新密钥为JSON数组
					if strings.HasPrefix(strings.TrimSpace(channel.Key), "[") {
						array, err := getVertexArrayKeys(channel.Key)
						if err != nil {
							common.ApiErrorT(c, "Failed to parse appended keys: {{error}}", map[string]any{"error": err.Error()})
							return
						}
						newKeys = array
					} else {
						// 单个JSON密钥
						newKeys = []string{channel.Key}
					}
				} else {
					// 普通渠道的处理
					inputKeys := strings.SplitSeq(channel.Key, "\n")
					for key := range inputKeys {
						key = strings.TrimSpace(key)
						if key != "" {
							newKeys = append(newKeys, key)
						}
					}
				}

				seen := make(map[string]struct{}, len(existingKeys)+len(newKeys))
				for _, key := range existingKeys {
					normalized := strings.TrimSpace(key)
					if normalized == "" {
						continue
					}
					seen[normalized] = struct{}{}
				}
				dedupedNewKeys := make([]string, 0, len(newKeys))
				for _, key := range newKeys {
					normalized := strings.TrimSpace(key)
					if normalized == "" {
						continue
					}
					if _, ok := seen[normalized]; ok {
						continue
					}
					seen[normalized] = struct{}{}
					dedupedNewKeys = append(dedupedNewKeys, normalized)
				}

				allKeys := append(existingKeys, dedupedNewKeys...)
				channel.Key = strings.Join(allKeys, "\n")
			}
		case "replace":
			// 覆盖模式：直接使用新密钥（默认行为，不需要特殊处理）
		}
	}
	err = channel.Update()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	if proxyChanged {
		service.InvalidateProxyClient(originProxy)
	}
	// 记录变更的字段名（语言无关的字段标识），密钥仅记录"已更换"绝不记录内容。
	changedFields := make([]string, 0)
	if channel.Models != originChannel.Models {
		changedFields = append(changedFields, "models")
	}
	if channel.Group != originChannel.Group {
		changedFields = append(changedFields, "group")
	}
	if channel.Type != originChannel.Type {
		changedFields = append(changedFields, "type")
	}
	if !equalStringPtr(channel.BaseURL, originChannel.BaseURL) {
		changedFields = append(changedFields, "base_url")
	}
	if channel.Key != "" && channel.Key != originChannel.Key {
		changedFields = append(changedFields, "key")
	}
	updateAudit := map[string]any{
		"id":             channel.Id,
		"name":           channel.Name,
		"changed_fields": changedFields,
	}
	if baseURLFromPluginDefault {
		updateAudit["base_url_source"] = "plugin_default"
	}
	recordManageAudit(c, "channel.update", updateAudit)
	channel.Key = ""
	clearChannelInfo(&channel.Channel)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    channel,
	})
	return
}

func UpdateChannelStatus(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if !assertChannelIDManageable(c, id) {
		return
	}
	req := ChannelStatusRequest{}
	if err := c.ShouldBindJSON(&req); err != nil || !isManageableChannelStatus(req.Status) {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	changed := model.UpdateChannelStatus(id, "", req.Status, "manual operation")
	if changed {
		model.InitChannelCache()
		if req.Status != common.ChannelStatusEnabled {
			closeActiveChannelWebSockets([]int{id})
		}
	}
	recordManageAudit(c, "channel.status_update", map[string]any{
		"id":      id,
		"status":  req.Status,
		"changed": changed,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    changed,
	})
}

func BatchUpdateChannelStatus(c *gin.Context) {
	req := ChannelStatusBatchRequest{}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Ids) == 0 || !isManageableChannelStatus(req.Status) {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if !assertChannelIDsManageable(c, req.Ids) {
		return
	}
	changedCount := 0
	var disabledIDs []int
	for _, id := range req.Ids {
		if model.UpdateChannelStatus(id, "", req.Status, "manual batch operation") {
			changedCount++
			if req.Status != common.ChannelStatusEnabled {
				disabledIDs = append(disabledIDs, id)
			}
		}
	}
	if changedCount > 0 {
		model.InitChannelCache()
	}
	if len(disabledIDs) > 0 {
		closeActiveChannelWebSockets(disabledIDs)
	}
	recordManageAudit(c, "channel.status_update_batch", map[string]any{
		"count":  changedCount,
		"total":  len(req.Ids),
		"status": req.Status,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    changedCount,
	})
}

func isManageableChannelStatus(status int) bool {
	return status == common.ChannelStatusEnabled || status == common.ChannelStatusManuallyDisabled
}

// equalStringPtr 比较两个 *string 是否相等（均为 nil 视为相等）。
func equalStringPtr(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

type fetchModelsRequest struct {
	ChannelID      int     `json:"channel_id"`
	BaseURL        *string `json:"base_url"`
	Type           int     `json:"type"`
	Key            string  `json:"key"`
	AdvancedCustom *string `json:"advanced_custom"`
	HeaderOverride *string `json:"header_override"`
	Proxy          *string `json:"proxy"`
}

func buildAdvancedCustomModelPreviewChannel(req fetchModelsRequest) (*model.Channel, error) {
	var channel *model.Channel
	if req.ChannelID > 0 {
		savedChannel, err := model.GetChannelById(req.ChannelID, true)
		if err != nil {
			return nil, err
		}
		if savedChannel.Type != constant.ChannelTypeAdvancedCustom {
			return nil, fmt.Errorf("channel %d is not an advanced custom channel", req.ChannelID)
		}
		channel = savedChannel
	} else {
		key := strings.TrimSpace(req.Key)
		if key != "" {
			key = strings.Split(key, "\n")[0]
		}
		channel = &model.Channel{
			Type: req.Type,
			Key:  key,
		}
	}

	if channel.Type != constant.ChannelTypeAdvancedCustom {
		return nil, fmt.Errorf("channel type must be advanced custom")
	}
	if req.BaseURL != nil {
		baseURL := strings.TrimSpace(*req.BaseURL)
		channel.BaseURL = &baseURL
	}

	settings := channel.GetOtherSettings()
	if req.AdvancedCustom != nil {
		rawConfig := strings.TrimSpace(*req.AdvancedCustom)
		if rawConfig == "" {
			return nil, fmt.Errorf("advanced_custom is required")
		}
		var config dto.AdvancedCustomConfig
		if err := common.UnmarshalJsonStr(rawConfig, &config); err != nil {
			return nil, err
		}
		settings.AdvancedCustom = &config
	} else if req.ChannelID <= 0 {
		return nil, fmt.Errorf("advanced_custom is required")
	}
	channel.SetOtherSettings(settings)

	if req.HeaderOverride != nil {
		rawHeaderOverride := strings.TrimSpace(*req.HeaderOverride)
		if rawHeaderOverride != "" {
			var headerOverride map[string]any
			if err := common.UnmarshalJsonStr(rawHeaderOverride, &headerOverride); err != nil {
				return nil, fmt.Errorf("header_override must be a JSON object: %w", err)
			}
		}
		channel.HeaderOverride = &rawHeaderOverride
	}
	if req.Proxy != nil {
		channelSettings := channel.GetSetting()
		channelSettings.Proxy = strings.TrimSpace(*req.Proxy)
		channel.SetSetting(channelSettings)
	}

	if err := validateChannel(channel, false); err != nil {
		return nil, err
	}
	return channel, nil
}

func FetchModels(c *gin.Context) {
	var req fetchModelsRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid request",
		})
		return
	}

	var channel *model.Channel
	if req.Type == constant.ChannelTypeAdvancedCustom || req.ChannelID > 0 {
		var err error
		channel, err = buildAdvancedCustomModelPreviewChannel(req)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	} else {
		baseURL := ""
		if req.BaseURL != nil {
			baseURL = strings.TrimSpace(*req.BaseURL)
		}
		if baseURL == "" {
			baseURL = constant.GetChannelBaseURL(req.Type)
		}

		key := strings.TrimSpace(req.Key)
		if req.Type != constant.ChannelTypeCodex {
			key = strings.Split(key, "\n")[0]
		}
		channel = &model.Channel{
			Type:    req.Type,
			Key:     key,
			BaseURL: &baseURL,
		}
	}

	models, err := fetchChannelUpstreamModelIDs(channel)
	if err != nil {
		common.ApiErrorT(c, "Failed to fetch models: {{error}}", map[string]any{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    models,
	})
}

func BatchSetChannelTag(c *gin.Context) {
	channelBatch := ChannelBatch{}
	err := c.ShouldBindJSON(&channelBatch)
	if err != nil || len(channelBatch.Ids) == 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if !assertChannelIDsManageable(c, channelBatch.Ids) {
		return
	}
	targetTag := ""
	if channelBatch.Tag != nil {
		targetTag = *channelBatch.Tag
	}
	if !assertTagManageable(c, targetTag) {
		return
	}
	err = model.BatchSetChannelTag(channelBatch.Ids, channelBatch.Tag)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	recordManageAudit(c, "channel.tag_batch_set", map[string]any{
		"count": len(channelBatch.Ids),
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    len(channelBatch.Ids),
	})
	return
}

func GetTagModels(c *gin.Context) {
	tag := c.Query("tag")
	if tag == "" {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Tag cannot be empty"))
		return
	}

	if !assertTagManageable(c, tag) {
		return
	}

	channels, err := model.GetChannelsByTag(tag, false, false) // idSort=false, selectAll=false
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	var longestModels string
	maxLength := 0

	// Find the longest models string among all channels with the given tag
	for _, channel := range channels {
		if channel.Models != "" {
			currentModels := strings.Split(channel.Models, ",")
			if len(currentModels) > maxLength {
				maxLength = len(currentModels)
				longestModels = channel.Models
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    longestModels,
	})
	return
}

// CopyChannel handles cloning an existing channel with its key.
// POST /api/channel/copy/:id
// Optional query params:
//
//	suffix         - string appended to the original name (default "_copy")
//	reset_balance  - bool, when true will reset balance & used_quota to 0 (default true)
func CopyChannel(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "invalid id"})
		return
	}

	if !assertChannelIDManageable(c, id) {
		return
	}

	suffix := c.DefaultQuery("suffix", "_copy")
	resetBalance := true
	if rbStr := c.DefaultQuery("reset_balance", "true"); rbStr != "" {
		if v, err := strconv.ParseBool(rbStr); err == nil {
			resetBalance = v
		}
	}

	// fetch original channel with key
	origin, err := model.GetChannelById(id, true)
	if err != nil {
		common.SysError(common.LogText("failed to get channel by id: %s", err.Error()))
		common.ApiErrorT(c, "Failed to fetch channel. Please try again later")
		return
	}
	if (origin.Type == constant.ChannelTypeTaskPlugin || len(origin.GetSetting().TaskPluginBindings()) > 0) &&
		!middleware.PermissionAllowed(c, authz.TaskPluginBind) {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "task plugin channels require the task_plugin.bind permission"})
		return
	}

	// clone channel
	clone := *origin // shallow copy is sufficient as we will overwrite primitives
	clone.Id = 0     // let DB auto-generate
	clone.CreatedTime = common.GetTimestamp()
	clone.Name = origin.Name + suffix
	clone.TestTime = 0
	clone.ResponseTime = 0
	if resetBalance {
		clone.Balance = 0
		clone.UsedQuota = 0
	}

	if err := clone.ValidateSettings(); err != nil {
		common.SysError(common.LogText("failed to validate cloned channel: %s", err.Error()))
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "Failed to copy channel: invalid channel settings"})
		return
	}

	// insert
	if err := clone.Insert(); err != nil {
		common.SysError(common.LogText("failed to clone channel: %s", err.Error()))
		common.ApiErrorT(c, "Failed to copy channel. Please try again later")
		return
	}
	model.InitChannelCache()
	recordManageAudit(c, "channel.copy", map[string]any{
		"sourceId": id,
		"id":       clone.Id,
		"name":     clone.Name,
	})
	// success
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"id": clone.Id}})
}

const (
	// multiKeyStatusMaxPageSize 限制 get_key_status 单页返回的密钥数：reason 携带完整上游错误体，
	// 无上限的 page_size 会把响应放大到 MB 级。前端配套逐页拉取，两端 clamp 保持一致。
	multiKeyStatusMaxPageSize = 500
	// multiKeyAuditIndexLimit 是审计 params 里逐个记录索引的上限，超过则只记 truncated 标记，
	// 避免一次全选把整个索引数组写进日志。
	multiKeyAuditIndexLimit = 50
	// multiKeyManualDisableReason 是禁用类操作未携带 reason 时写入的兜底原因。
	// 该串会存进 ChannelInfo.MultiKeyDisabledReason 并原样展示，用于与自动禁用写入的上游错误体区分。
	multiKeyManualDisableReason = "Disabled by admin"
)

// MultiKeyManageRequest represents the request for multi-key management operations
type MultiKeyManageRequest struct {
	ChannelId int `json:"channel_id"`
	// Action 的完整取值，与 ManageMultiKeys 的 switch 一一对应：
	//   单密钥（需 key_index）：disable_key / enable_key / delete_key
	//   多选批量（需 key_indexes）：enable_keys / disable_keys / delete_keys
	//   整渠道：enable_all_keys / disable_all_keys / delete_disabled_keys
	//   只读：get_key_status
	Action   string `json:"action"`
	KeyIndex *int   `json:"key_index,omitempty"` // for disable_key, enable_key, and delete_key actions
	Page     int    `json:"page,omitempty"`      // for get_key_status pagination
	PageSize int    `json:"page_size,omitempty"` // for get_key_status pagination
	Status   *int   `json:"status,omitempty"`    // for get_key_status filtering: 1=enabled, 2=manual_disabled, 3=auto_disabled, nil=all
	// 批量操作 enable_keys / disable_keys / delete_keys 的目标索引，去重与边界校验见 resolveMultiKeyTargetIndexes
	KeyIndexes []int `json:"key_indexes,omitempty"`
	// 必须用 binding 而非 validate：gin 的 defaultValidator 调了 validate.SetTagName("binding")，
	// validate: tag 对 c.ShouldBindJSON 完全不可见。
	Reason string `json:"reason,omitempty" binding:"max=255"`
	// ExpectedKeyCount 是全部六个按索引操作的并发前置条件——三个单密钥
	// （disable_key / enable_key / delete_key）与三个批量（enable_keys / disable_keys / delete_keys）
	// 一视同仁：行内操作的索引同样来自一份可能过期的列表快照，重编号后指向的已不是操作员看到的密钥。
	// 在锁内与真实密钥数比对，不等则整个请求拒绝且不做任何写入；为 nil 时跳过校验。
	// 三个整渠道操作（enable_all_keys / disable_all_keys / delete_disabled_keys）刻意不校验：
	// 它们不携带操作员挑选的索引列表，没有可被重编号改写的目标。
	// 已知残留限制：只比对数量，所以保数量的变更——删一个再追加一个、或等长的 KeyMode='replace'
	// ——仍能通过校验；要堵住它需要由各改写路径维护的密钥列表版本号，不在本字段的能力范围内。
	ExpectedKeyCount *int `json:"expected_key_count,omitempty"`
}

// MultiKeyStatusResponse represents the response for key status query
type MultiKeyStatusResponse struct {
	Keys       []KeyStatus `json:"keys"`
	Total      int         `json:"total"`
	Page       int         `json:"page"`
	PageSize   int         `json:"page_size"`
	TotalPages int         `json:"total_pages"`
	// Statistics
	EnabledCount        int `json:"enabled_count"`
	ManualDisabledCount int `json:"manual_disabled_count"`
	AutoDisabledCount   int `json:"auto_disabled_count"`
}

type KeyStatus struct {
	Index        int    `json:"index"`
	Status       int    `json:"status"` // 1: enabled, 2: disabled
	DisabledTime int64  `json:"disabled_time,omitempty"`
	Reason       string `json:"reason,omitempty"`
	KeyPreview   string `json:"key_preview"` // first 10 chars of key for identification
}

// handleGetKeyStatus 处理只读的 get_key_status，不持有渠道轮询锁：在锁内做 DB 读 + 构建列表 +
// JSON 序列化写 socket 会阻塞 relay 的密钥轮询，而无上限的 page_size 更会把它变成持锁 DoS。
// 代价是三个统计计数与返回行来自同一个 pre-lock 快照，相对并发写可能略微过期——
// 对管理对话框可接受，不要把锁加回来。
func handleGetKeyStatus(c *gin.Context, request MultiKeyManageRequest) {
	channel, err := model.GetChannelById(request.ChannelId, true)
	if err != nil {
		common.ApiErrorT(c, "Channel does not exist")
		return
	}
	if !channel.ChannelInfo.IsMultiKey {
		common.ApiErrorT(c, "This channel is not in multi-key mode")
		return
	}

	keys := channel.GetKeys()

	// Default pagination parameters
	page := request.Page
	pageSize := request.PageSize
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 50 // Default page size
	}
	if pageSize > multiKeyStatusMaxPageSize {
		pageSize = multiKeyStatusMaxPageSize
	}

	// Statistics for all keys (unchanged by filtering)
	var enabledCount, manualDisabledCount, autoDisabledCount int

	// Build all key status data first
	var allKeyStatusList []KeyStatus
	for i, key := range keys {
		status := 1 // default enabled
		var disabledTime int64
		var reason string

		if channel.ChannelInfo.MultiKeyStatusList != nil {
			if s, exists := channel.ChannelInfo.MultiKeyStatusList[i]; exists {
				status = s
			}
		}

		// Count for statistics (all keys)
		switch status {
		case 1:
			enabledCount++
		case 2:
			manualDisabledCount++
		case 3:
			autoDisabledCount++
		}

		if status != 1 {
			if channel.ChannelInfo.MultiKeyDisabledTime != nil {
				disabledTime = channel.ChannelInfo.MultiKeyDisabledTime[i]
			}
			if channel.ChannelInfo.MultiKeyDisabledReason != nil {
				reason = channel.ChannelInfo.MultiKeyDisabledReason[i]
			}
		}

		// Create key preview (first 10 chars)
		keyPreview := key
		if len(key) > 10 {
			keyPreview = key[:10] + "..."
		}

		allKeyStatusList = append(allKeyStatusList, KeyStatus{
			Index:        i,
			Status:       status,
			DisabledTime: disabledTime,
			Reason:       reason,
			KeyPreview:   keyPreview,
		})
	}

	// Apply status filter if specified
	var filteredKeyStatusList []KeyStatus
	if request.Status != nil {
		for _, keyStatus := range allKeyStatusList {
			if keyStatus.Status == *request.Status {
				filteredKeyStatusList = append(filteredKeyStatusList, keyStatus)
			}
		}
	} else {
		filteredKeyStatusList = allKeyStatusList
	}

	// Calculate pagination based on filtered results
	filteredTotal := len(filteredKeyStatusList)
	totalPages := (filteredTotal + pageSize - 1) / pageSize
	if totalPages == 0 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}

	// Calculate range for current page
	start := (page - 1) * pageSize
	end := start + pageSize
	if end > filteredTotal {
		end = filteredTotal
	}

	// Get the page data
	var pageKeyStatusList []KeyStatus
	if start < filteredTotal {
		pageKeyStatusList = filteredKeyStatusList[start:end]
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": MultiKeyStatusResponse{
			Keys:                pageKeyStatusList,
			Total:               filteredTotal, // Total of filtered results
			Page:                page,
			PageSize:            pageSize,
			TotalPages:          totalPages,
			EnabledCount:        enabledCount,        // Overall statistics
			ManualDisabledCount: manualDisabledCount, // Overall statistics
			AutoDisabledCount:   autoDisabledCount,   // Overall statistics
		},
	})
}

// 多密钥管理业务拒绝的 error_code：稳定、机器可读，前端据此拼 i18n 文案，
// message 只作非 UI 客户端的兜底。批量与单密钥 action 共用这一套机制——
// 单密钥拒绝同样必须带 code，否则前端只能把中文 message 原样 toast 给非中文管理员。
const (
	multiKeyErrCodeKeyListChanged     = "key_list_changed"
	multiKeyErrCodeIndexesEmpty       = "key_indexes_empty"
	multiKeyErrCodeIndexOutOfRange    = "key_index_out_of_range"
	multiKeyErrCodeNoKeysToDisable    = "no_keys_to_disable"
	multiKeyErrCodeCannotDeleteAll    = "cannot_delete_all_keys"
	multiKeyErrCodeAllKeysDisabled    = "all_keys_auto_disabled"
	multiKeyErrCodeKeyAlreadyDisabled = "key_already_disabled"
)

// multiKeyBatchFailure 是多密钥 action 的业务拒绝：code 供前端拼 i18n 文案，
// message 是给非 UI 客户端的中文兜底。刻意不实现 error 接口——这些不是异常，
// 而是预期内的拒绝，一律走 respondMultiKeyBatchError 输出 HTTP 200 + success:false。
type multiKeyBatchFailure struct {
	code    string
	message string
}

// respondMultiKeyBatchError 统一业务拒绝的响应形状。counts 为 nil 时不写 data：
// 索引校验类失败没有可信的计数可报。绝不用 common.ApiError——那会变成 500，
// 而前端对这些 action 开了 skipBusinessError，业务失败以 data 形式 resolve。
// 名字里的 Batch 是历史沿革：响应形状与 action 涉及几个密钥无关，单密钥拒绝
// （key_already_disabled）也走这里，而不是复制一个逐字节相同的 sibling。
func respondMultiKeyBatchError(c *gin.Context, failure multiKeyBatchFailure, counts gin.H) {
	response := gin.H{
		"success":    false,
		"error_code": failure.code,
		"message":    failure.message,
	}
	if counts != nil {
		response["data"] = counts
	}
	c.JSON(http.StatusOK, response)
}

// checkMultiKeyListUnchanged 是六个按索引操作共用的并发前置条件，比对不通过时返回待响应的拒绝值，
// 通过（或调用方未携带该字段）时返回 nil。六处共用同一份比对与同一句 message，
// 否则任一分支被改写都会让"陈旧列表"在不同 action 上表现不一致。
// 参数取 keys 而非计数：比对只允许对着 GetKeys() 这个 ground truth 发生，
// ChannelInfo.MultiKeySize 是可能被 UpdateChannelStatus 的无锁 read-modify-write 污染的派生值。
func checkMultiKeyListUnchanged(expected *int, keys []string) *multiKeyBatchFailure {
	if expected == nil || *expected == len(keys) {
		return nil
	}
	return &multiKeyBatchFailure{
		code:    multiKeyErrCodeKeyListChanged,
		message: "密钥列表已变更，请刷新后重试",
	}
}

// resolveMultiKeyTargetIndexes 去重并按 GetKeys() 的长度做边界校验。
// 用 len(keys) 而非 ChannelInfo.MultiKeySize：前者是 ground truth，后者是缓存派生值，
// 可能被 UpdateChannelStatus 的无锁 read-modify-write 污染。
// 越界或负数一律整批拒绝，绝不静默截断。
func resolveMultiKeyTargetIndexes(keys []string, indexes []int) (map[int]struct{}, *multiKeyBatchFailure) {
	if len(indexes) == 0 {
		return nil, &multiKeyBatchFailure{
			code:    multiKeyErrCodeIndexesEmpty,
			message: "密钥索引列表为空",
		}
	}
	set := make(map[int]struct{}, len(indexes))
	for _, idx := range indexes {
		if idx < 0 || idx >= len(keys) {
			return nil, &multiKeyBatchFailure{
				code:    multiKeyErrCodeIndexOutOfRange,
				message: fmt.Sprintf("密钥索引超出范围: %d", idx),
			}
		}
		set[idx] = struct{}{}
	}
	return set, nil
}

// multiKeyDisableReason 返回禁用类操作实际写入的原因，请求未携带时回退到固定兜底串。
func multiKeyDisableReason(request MultiKeyManageRequest) string {
	if request.Reason != "" {
		return request.Reason
	}
	return multiKeyManualDisableReason
}

// ManageMultiKeys handles multi-key management operations
func ManageMultiKeys(c *gin.Context) {
	request := MultiKeyManageRequest{}
	err := c.ShouldBindJSON(&request)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 标签白名单校验必须在加载渠道之前，所以用 ID 版本；不受限管理员零额外查询。
	if !assertChannelIDManageable(c, request.ChannelId) {
		return
	}
	if multiKeyActionRequiresSensitiveWrite(request.Action) &&
		!middleware.PermissionAllowed(c, authz.ChannelSensitiveWrite) {
		common.ApiErrorI18n(c, i18n.MsgAuthInsufficientPrivilege)
		return
	}

	// get_key_status 为只读查询，不记录审计；其余为修改操作，记录审计并跳过中间件兜底。
	// 审计留在锁外：同步 DB insert 不该压在 relay 热路径依赖的渠道轮询锁里。
	switch request.Action {
	case "get_key_status":
		markAuditLogged(c)
	case "enable_keys", "disable_keys", "delete_keys":
		// 批量操作用独立 action id：os.Expand 对缺失 key 替换成空串，
		// 共享 channel.multi_key_manage 模板带不了 ${count}。
		params := map[string]interface{}{
			"action": request.Action,
			"id":     request.ChannelId,
			// 去重与边界校验发生在审计之后，所以这里记的是请求数量而非实际生效数量
			"count": len(request.KeyIndexes),
		}
		if len(request.KeyIndexes) <= multiKeyAuditIndexLimit {
			params["key_indexes"] = request.KeyIndexes
		} else {
			params["truncated"] = true
		}
		if request.Action == "disable_keys" {
			params["reason"] = request.Reason
		}
		// 绝不写入密钥本体、preview 或摘要：op.params 是用户可见的非敏感字段。
		recordManageAudit(c, "channel.multi_key_batch_manage", params)
	default:
		recordManageAudit(c, "channel.multi_key_manage", map[string]interface{}{
			"action": request.Action,
			"id":     request.ChannelId,
		})
	}

	// 纯读操作在加锁之前 return
	if request.Action == "get_key_status" {
		handleGetKeyStatus(c, request)
		return
	}

	// 所有修改操作：先锁后读。反过来（锁外读、锁内写）等于对 read-modify-write 完全无保护，
	// 两个并发请求都基于同一 pre-state 计算，批量操作会把"丢 1 个密钥"放大成"丢 N 个密钥"。
	lock := model.GetChannelPollingLock(request.ChannelId)
	lock.Lock()
	defer lock.Unlock()

	channel, err := model.GetChannelById(request.ChannelId, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "渠道不存在",
		})
		return
	}

	if !channel.ChannelInfo.IsMultiKey {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "该渠道不是多密钥模式",
		})
		return
	}

	switch request.Action {
	case "disable_key":
		if request.KeyIndex == nil {
			common.ApiErrorT(c, "No key index specified to disable")
			return
		}

		// 并发前置条件：行内操作与批量同样受重编号影响。管理对话框不随窗口聚焦刷新也没有轮询，
		// 一份数小时前的快照上点"禁用 #9"，会把操作员为 #9 审阅通过的 reason 盖到如今坐在索引 8 的
		// 那个密钥上。必须排在边界校验之前：陈旧列表要报成陈旧列表而不是索引越界。
		if staleFailure := checkMultiKeyListUnchanged(request.ExpectedKeyCount, channel.GetKeys()); staleFailure != nil {
			respondMultiKeyBatchError(c, *staleFailure, nil)
			return
		}

		keyIndex := *request.KeyIndex
		if keyIndex < 0 || keyIndex >= channel.ChannelInfo.MultiKeySize {
			common.ApiErrorT(c, "Key index out of range")
			return
		}

		// 走 DisableKeys 统一写入 status/reason/time，消掉三处 3-map 写入的漂移风险；
		// 已处于禁用态的索引会被跳过，其上游诊断信息不被覆盖。
		if channel.ChannelInfo.DisableKeys([]int{keyIndex}, multiKeyDisableReason(request)) == 0 {
			// 该密钥已是禁用态（很可能是线上流量触发的自动禁用），DisableKeys 有意跳过它。
			// 不能照旧回报"密钥已禁用"：那会宣称一次并未发生的手动禁用，
			// 而该密钥实际仍带着上游的禁用原因与旧时间戳。同时跳过 Update() 与缓存刷新。
			// 必须带 error_code：前端据此提示刷新并重新拉取该行，否则这条中文 message
			// 会落到 getMultiKeyFailureMessage 的 default 分支被原样 toast 出去。
			respondMultiKeyBatchError(c, multiKeyBatchFailure{
				code:    multiKeyErrCodeKeyAlreadyDisabled,
				message: "该密钥已处于禁用状态",
			}, nil)
			return
		}

		channel.NormalizeMultiKeyStatus()
		shouldCloseWebSocket := disableMultiKeyChannelIfUnavailable(channel)
		err = channel.Update()
		if err != nil {
			common.ApiError(c, err)
			return
		}
		model.InitChannelCache()
		if shouldCloseWebSocket {
			closeActiveChannelWebSockets([]int{channel.Id})
		}
		common.ApiSuccessT(c, "Key disabled", nil)
		return

	case "enable_key":
		if request.KeyIndex == nil {
			common.ApiErrorT(c, "No key index specified to enable")
			return
		}

		// 同 disable_key：陈旧列表上的行内启用会把上游自动禁用的诊断从另一个密钥身上抹掉，
		// 并把它悄无声息地放回轮询。排在边界校验之前，理由同上。
		if staleFailure := checkMultiKeyListUnchanged(request.ExpectedKeyCount, channel.GetKeys()); staleFailure != nil {
			respondMultiKeyBatchError(c, *staleFailure, nil)
			return
		}

		keyIndex := *request.KeyIndex
		if keyIndex < 0 || keyIndex >= channel.ChannelInfo.MultiKeySize {
			common.ApiErrorT(c, "Key index out of range")
			return
		}

		// 从状态列表中删除该密钥的记录，使其回到默认启用状态
		if channel.ChannelInfo.MultiKeyStatusList != nil {
			delete(channel.ChannelInfo.MultiKeyStatusList, keyIndex)
		}
		if channel.ChannelInfo.MultiKeyDisabledTime != nil {
			delete(channel.ChannelInfo.MultiKeyDisabledTime, keyIndex)
		}
		if channel.ChannelInfo.MultiKeyDisabledReason != nil {
			delete(channel.ChannelInfo.MultiKeyDisabledReason, keyIndex)
		}
		restoreMultiKeyChannelIfAvailable(channel)

		channel.NormalizeMultiKeyStatus()
		err = channel.Update()
		if err != nil {
			common.ApiError(c, err)
			return
		}

		model.InitChannelCache()
		common.ApiSuccessT(c, "Key enabled", nil)
		return

	case "enable_keys":
		keys := channel.GetKeys()
		// 并发前置条件：可逆的 action 同样需要它。重编号会让批量启用落到操作员没选的索引上，
		// 顺手抹掉那批密钥的上游禁用诊断并把它们放回轮询，而 UI 上没有任何可靠痕迹告知这次误写。
		// 必须排在 resolveMultiKeyTargetIndexes 之前，陈旧列表要报成陈旧列表而不是索引越界——
		// 两种拒绝对操作员意味着不同的事，前端各有文案。
		if staleFailure := checkMultiKeyListUnchanged(request.ExpectedKeyCount, keys); staleFailure != nil {
			respondMultiKeyBatchError(c, *staleFailure, nil)
			return
		}

		targets, resolveFailure := resolveMultiKeyTargetIndexes(keys, request.KeyIndexes)
		if resolveFailure != nil {
			respondMultiKeyBatchError(c, *resolveFailure, nil)
			return
		}

		// 与 enable_key 对齐：从三张 map 里 delete 而不是写 enabled。渠道列表用
		// Object.keys(multi_key_status_list).length 统计禁用数，写 1 会让徽章少报。
		var affected int
		for idx := range targets {
			if status, exists := channel.ChannelInfo.MultiKeyStatusList[idx]; exists && status != common.ChannelStatusEnabled {
				affected++
			}
			delete(channel.ChannelInfo.MultiKeyStatusList, idx)
			delete(channel.ChannelInfo.MultiKeyDisabledTime, idx)
			delete(channel.ChannelInfo.MultiKeyDisabledReason, idx)
		}

		restoreMultiKeyChannelIfAvailable(channel)
		channel.NormalizeMultiKeyStatus()
		err = channel.Update()
		if err != nil {
			common.ApiError(c, err)
			return
		}

		model.InitChannelCache()
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "",
			"data": gin.H{
				"affected_count":  affected,
				"skipped_count":   len(targets) - affected,
				"remaining_count": len(channel.GetKeys()),
			},
		})
		return

	case "disable_keys":
		keys := channel.GetKeys()
		// 同 enable_keys：陈旧列表上的批量禁用整批拒绝。重编号会把操作员选中的 #4/#7/#10
		// 变成对如今坐在 3/6/9 的密钥的手动禁用，还把操作员为另一批密钥审阅通过的 reason
		// 原样盖上去；事后只能逐行比对 reason 才能发现，UI 无从提示。
		if staleFailure := checkMultiKeyListUnchanged(request.ExpectedKeyCount, keys); staleFailure != nil {
			respondMultiKeyBatchError(c, *staleFailure, nil)
			return
		}

		targets, resolveFailure := resolveMultiKeyTargetIndexes(keys, request.KeyIndexes)
		if resolveFailure != nil {
			respondMultiKeyBatchError(c, *resolveFailure, nil)
			return
		}

		sortedIndexes := make([]int, 0, len(targets))
		for idx := range targets {
			sortedIndexes = append(sortedIndexes, idx)
		}
		sort.Ints(sortedIndexes)

		affected := channel.ChannelInfo.DisableKeys(sortedIndexes, multiKeyDisableReason(request))
		if affected == 0 {
			// 镜像 disable_all_keys：没有任何变更时跳过 Update() 与缓存刷新。
			// 计数照常返回，前端才能说清"选中的都已处于禁用状态"而不是笼统报错。
			respondMultiKeyBatchError(c, multiKeyBatchFailure{
				code:    multiKeyErrCodeNoKeysToDisable,
				message: "没有可禁用的密钥",
			}, gin.H{
				"affected_count":  0,
				"skipped_count":   len(targets),
				"remaining_count": len(channel.GetKeys()),
			})
			return
		}

		channel.NormalizeMultiKeyStatus()
		shouldCloseWebSocket := disableMultiKeyChannelIfUnavailable(channel)
		err = channel.Update()
		if err != nil {
			common.ApiError(c, err)
			return
		}

		model.InitChannelCache()
		if shouldCloseWebSocket {
			closeActiveChannelWebSockets([]int{channel.Id})
		}
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "",
			"data": gin.H{
				"affected_count":  affected,
				"skipped_count":   len(targets) - affected,
				"remaining_count": len(channel.GetKeys()),
			},
		})
		return

	case "enable_all_keys":
		// 清空所有禁用状态，使所有密钥回到默认启用状态
		var enabledCount int
		if channel.ChannelInfo.MultiKeyStatusList != nil {
			enabledCount = len(channel.ChannelInfo.MultiKeyStatusList)
		}

		channel.ChannelInfo.MultiKeyStatusList = make(map[int]int)
		channel.ChannelInfo.MultiKeyDisabledTime = make(map[int]int64)
		channel.ChannelInfo.MultiKeyDisabledReason = make(map[int]string)
		restoreMultiKeyChannelIfAvailable(channel)

		channel.NormalizeMultiKeyStatus()
		err = channel.Update()
		if err != nil {
			common.ApiError(c, err)
			return
		}

		model.InitChannelCache()
		common.ApiSuccessT(c, "Enabled {{count}} keys", nil, map[string]any{"count": enabledCount})
		return

	case "disable_all_keys":
		// 禁用所有启用的密钥。DisableKeys 自身跳过已禁用的索引，所以直接传全量索引；
		// 循环边界保持 MultiKeySize 不变。
		allIndexes := make([]int, 0, channel.ChannelInfo.MultiKeySize)
		for i := 0; i < channel.ChannelInfo.MultiKeySize; i++ {
			allIndexes = append(allIndexes, i)
		}
		disabledCount := channel.ChannelInfo.DisableKeys(allIndexes, multiKeyDisableReason(request))

		if disabledCount == 0 {
			common.ApiErrorT(c, "No keys to disable")
			return
		}

		channel.NormalizeMultiKeyStatus()
		shouldCloseWebSocket := disableMultiKeyChannelIfUnavailable(channel)
		err = channel.Update()
		if err != nil {
			common.ApiError(c, err)
			return
		}
		model.InitChannelCache()
		if shouldCloseWebSocket {
			closeActiveChannelWebSockets([]int{channel.Id})
		}
		common.ApiSuccessT(c, "Disabled {{count}} keys", nil, map[string]any{"count": disabledCount})
		return

	case "delete_key":
		if request.KeyIndex == nil {
			common.ApiErrorT(c, "No key index specified to delete")
			return
		}

		// 同 disable_key，且后果不可逆：陈旧快照上的行内删除会永久抹掉另一个密钥。
		// 排在边界校验之前，理由同上。
		if staleFailure := checkMultiKeyListUnchanged(request.ExpectedKeyCount, channel.GetKeys()); staleFailure != nil {
			respondMultiKeyBatchError(c, *staleFailure, nil)
			return
		}

		keyIndex := *request.KeyIndex
		if keyIndex < 0 || keyIndex >= channel.ChannelInfo.MultiKeySize {
			common.ApiErrorT(c, "Key index out of range")
			return
		}

		// The caller prevents removing every key; RemoveMultiKeys only rewrites indexed state.
		if len(channel.GetKeys()) <= 1 {
			common.ApiErrorT(c, "Cannot delete the last key")
			return
		}

		channel.RemoveMultiKeys(map[int]struct{}{keyIndex: {}})

		channel.NormalizeMultiKeyStatus()
		shouldCloseWebSocket := disableMultiKeyChannelIfUnavailable(channel)
		err = channel.Update()
		if err != nil {
			common.ApiError(c, err)
			return
		}
		model.InitChannelCache()
		if shouldCloseWebSocket {
			closeActiveChannelWebSockets([]int{channel.Id})
		}
		common.ApiSuccessT(c, "Key deleted", nil)
		return

	case "delete_keys":
		keys := channel.GetKeys()
		// 并发前置条件：陈旧列表上的批量删除必须整批失败，否则重编号会让删除落到别的密钥上
		if staleFailure := checkMultiKeyListUnchanged(request.ExpectedKeyCount, keys); staleFailure != nil {
			respondMultiKeyBatchError(c, *staleFailure, nil)
			return
		}

		targets, resolveFailure := resolveMultiKeyTargetIndexes(keys, request.KeyIndexes)
		if resolveFailure != nil {
			respondMultiKeyBatchError(c, *resolveFailure, nil)
			return
		}
		// 同样把"不能删空"留在 caller；返回 200 + success:false 而不是 common.ApiError（那会变 500）
		if len(targets) >= len(keys) {
			respondMultiKeyBatchError(c, multiKeyBatchFailure{
				code:    multiKeyErrCodeCannotDeleteAll,
				message: "不能删除最后一个密钥",
			}, gin.H{
				"affected_count":  0,
				"skipped_count":   len(targets),
				"remaining_count": len(keys),
			})
			return
		}

		removed := channel.RemoveMultiKeys(targets)

		channel.NormalizeMultiKeyStatus()
		shouldCloseWebSocket := disableMultiKeyChannelIfUnavailable(channel)
		err = channel.Update()
		if err != nil {
			common.ApiError(c, err)
			return
		}

		model.InitChannelCache()
		if shouldCloseWebSocket {
			closeActiveChannelWebSockets([]int{channel.Id})
		}
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "",
			"data": gin.H{
				// 数量取自 RemoveMultiKeys 的返回值而非 len(targets)
				"affected_count":  removed,
				"skipped_count":   len(targets) - removed,
				"remaining_count": len(channel.GetKeys()),
			},
		})
		return

	case "delete_disabled_keys":
		keys := channel.GetKeys()
		deleted := make(map[int]struct{})
		for i := range keys {
			// 只删除自动禁用（status == 3）的密钥，保留启用与手动禁用的密钥
			if channel.ChannelInfo.MultiKeyStatusList[i] == common.ChannelStatusAutoDisabled {
				deleted[i] = struct{}{}
			}
		}

		if len(deleted) == 0 {
			common.ApiErrorT(c, "No auto-disabled keys to delete")
			return
		}

		// 与 delete_key / delete_keys 一致地拒绝删空。删空这条路径本身就是坏的：
		// RemoveMultiKeys 会把 channel.Key 置空，Update() 的 channel.Key == "" 分支转而从 DB
		// 读回旧密钥列表，而 DB.Updates(channel) 又会跳过零值 Key —— 密钥一个都删不掉，
		// 但三张 map 已被清空、NormalizeMultiKeyStatus 还会把渠道翻成自动禁用并关掉 abilities。
		// 净效果是"密钥没删、渠道下线"，所以这里直接拒绝并让管理员去删渠道本身。
		if len(deleted) >= len(keys) {
			respondMultiKeyBatchError(c, multiKeyBatchFailure{
				code:    multiKeyErrCodeAllKeysDisabled,
				message: "该渠道的全部密钥都已被自动禁用，不能全部删除；如需下线请直接删除该渠道",
			}, nil)
			return
		}

		deletedCount := channel.RemoveMultiKeys(deleted)

		channel.NormalizeMultiKeyStatus()
		shouldCloseWebSocket := disableMultiKeyChannelIfUnavailable(channel)
		err = channel.Update()
		if err != nil {
			common.ApiError(c, err)
			return
		}
		model.InitChannelCache()
		if shouldCloseWebSocket {
			closeActiveChannelWebSockets([]int{channel.Id})
		}
		common.ApiSuccessT(c, "Deleted {{count}} auto-disabled keys", deletedCount, map[string]any{"count": deletedCount})
		return

	default:
		common.ApiErrorT(c, "Unsupported operation")
		return
	}
}

func multiKeyActionRequiresSensitiveWrite(action string) bool {
	return action == "delete_key" || action == "delete_keys" || action == "delete_disabled_keys"
}

// OllamaPullModel 拉取 Ollama 模型
func OllamaPullModel(c *gin.Context) {
	var req struct {
		ChannelID int    `json:"channel_id"`
		ModelName string `json:"model_name"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid request parameters",
		})
		return
	}

	if req.ChannelID == 0 || req.ModelName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Channel ID and model name are required",
		})
		return
	}

	// 获取渠道信息
	channel, err := model.GetChannelById(req.ChannelID, true)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "Channel not found",
		})
		return
	}

	// 检查是否是 Ollama 渠道
	if channel.Type != constant.ChannelTypeOllama {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "This operation is only supported for Ollama channels",
		})
		return
	}

	baseURL := constant.GetChannelBaseURL(channel.Type)
	if channel.GetBaseURL() != "" {
		baseURL = channel.GetBaseURL()
	}

	key := strings.Split(channel.Key, "\n")[0]
	err = ollama.PullOllamaModel(baseURL, key, req.ModelName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": fmt.Sprintf("Failed to pull model: %s", err.Error()),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("Model %s pulled successfully", req.ModelName),
	})
}

// OllamaPullModelStream 流式拉取 Ollama 模型
func OllamaPullModelStream(c *gin.Context) {
	var req struct {
		ChannelID int    `json:"channel_id"`
		ModelName string `json:"model_name"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid request parameters",
		})
		return
	}

	if req.ChannelID == 0 || req.ModelName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Channel ID and model name are required",
		})
		return
	}

	// 获取渠道信息
	channel, err := model.GetChannelById(req.ChannelID, true)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "Channel not found",
		})
		return
	}

	// 检查是否是 Ollama 渠道
	if channel.Type != constant.ChannelTypeOllama {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "This operation is only supported for Ollama channels",
		})
		return
	}

	baseURL := constant.GetChannelBaseURL(channel.Type)
	if channel.GetBaseURL() != "" {
		baseURL = channel.GetBaseURL()
	}

	// 设置 SSE 头部
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")

	key := strings.Split(channel.Key, "\n")[0]

	// 创建进度回调函数
	progressCallback := func(progress ollama.OllamaPullResponse) {
		data, _ := json.Marshal(progress)
		fmt.Fprintf(c.Writer, "data: %s\n\n", string(data))
		c.Writer.Flush()
	}

	// 执行拉取
	err = ollama.PullOllamaModelStream(baseURL, key, req.ModelName, progressCallback)

	if err != nil {
		errorData, _ := json.Marshal(gin.H{
			"error": err.Error(),
		})
		fmt.Fprintf(c.Writer, "data: %s\n\n", string(errorData))
	} else {
		successData, _ := json.Marshal(gin.H{
			"message": fmt.Sprintf("Model %s pulled successfully", req.ModelName),
		})
		fmt.Fprintf(c.Writer, "data: %s\n\n", string(successData))
	}

	// 发送结束标志
	fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
	c.Writer.Flush()
}

// OllamaDeleteModel 删除 Ollama 模型
func OllamaDeleteModel(c *gin.Context) {
	var req struct {
		ChannelID int    `json:"channel_id"`
		ModelName string `json:"model_name"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid request parameters",
		})
		return
	}

	if req.ChannelID == 0 || req.ModelName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Channel ID and model name are required",
		})
		return
	}

	// 获取渠道信息
	channel, err := model.GetChannelById(req.ChannelID, true)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "Channel not found",
		})
		return
	}

	// 检查是否是 Ollama 渠道
	if channel.Type != constant.ChannelTypeOllama {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "This operation is only supported for Ollama channels",
		})
		return
	}

	baseURL := constant.GetChannelBaseURL(channel.Type)
	if channel.GetBaseURL() != "" {
		baseURL = channel.GetBaseURL()
	}

	key := strings.Split(channel.Key, "\n")[0]
	err = ollama.DeleteOllamaModel(baseURL, key, req.ModelName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": fmt.Sprintf("Failed to delete model: %s", err.Error()),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("Model %s deleted successfully", req.ModelName),
	})
}

// OllamaVersion 获取 Ollama 服务版本信息
func OllamaVersion(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid channel id",
		})
		return
	}

	channel, err := model.GetChannelById(id, true)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "Channel not found",
		})
		return
	}

	if channel.Type != constant.ChannelTypeOllama {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "This operation is only supported for Ollama channels",
		})
		return
	}

	baseURL := constant.GetChannelBaseURL(channel.Type)
	if channel.GetBaseURL() != "" {
		baseURL = channel.GetBaseURL()
	}

	key := strings.Split(channel.Key, "\n")[0]
	version, err := ollama.FetchOllamaVersion(baseURL, key)
	if err != nil {
		common.ApiErrorT(c, "Failed to fetch Ollama version: {{error}}", map[string]any{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"version": version,
		},
	})
}
