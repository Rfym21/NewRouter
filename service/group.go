package service

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"new-router/common"
	"new-router/constant"
	"new-router/model"
	"new-router/setting"
	"new-router/setting/ratio_setting"
)

// applySpecialUsableOverlay 按 +:/-: 前缀规则叠加到可用分组集合:
// "-:g" 移除分组 g;"+:g" 或无前缀 添加分组 g(desc 为展示描述)。
func applySpecialUsableOverlay(dst map[string]string, rules map[string]string) {
	for specialGroup, desc := range rules {
		if strings.HasPrefix(specialGroup, "-:") {
			delete(dst, strings.TrimPrefix(specialGroup, "-:"))
		} else if strings.HasPrefix(specialGroup, "+:") {
			dst[strings.TrimPrefix(specialGroup, "+:")] = desc
		} else {
			dst[specialGroup] = desc
		}
	}
}

// GetUserUsableGroups 返回某用户可用的分组集合。
// 叠加顺序:基础可用分组 → 按用户组特殊规则 → 按用户 ID 特殊规则(用户级可覆盖组级),
// 最后确保 userGroup 自身在集合内。userId<=0 时跳过用户级规则。
func GetUserUsableGroups(userGroup string, userIDs ...int) map[string]string {
	userId := 0
	if len(userIDs) > 0 {
		userId = userIDs[0]
	}
	groupsCopy := setting.GetUserUsableGroupsCopy()
	grSetting := ratio_setting.GetGroupRatioSetting()
	if userGroup != "" {
		if rules, ok := grSetting.GroupSpecialUsableGroup.Get(userGroup); ok {
			applySpecialUsableOverlay(groupsCopy, rules)
		}
	}
	if userId > 0 {
		if rules, ok := grSetting.UserSpecialUsableGroup.Get(strconv.Itoa(userId)); ok {
			applySpecialUsableOverlay(groupsCopy, rules)
		}
	}
	if userGroup != "" {
		// 如果 userGroup 不在可用分组中,补入 userGroup
		if _, ok := groupsCopy[userGroup]; !ok {
			groupsCopy[userGroup] = "User group"
		}
	}
	return groupsCopy
}

func GroupInUserUsableGroups(userGroup, groupName string, userIDs ...int) bool {
	_, ok := GetUserUsableGroups(userGroup, userIDs...)[groupName]
	return ok
}

func IsUserSelectableGroup(userGroup, groupName string, userIDs ...int) bool {
	if groupName == "" || groupName == "auto" {
		return false
	}
	return GroupInUserUsableGroups(userGroup, groupName, userIDs...) && ratio_setting.ContainsGroupRatio(groupName)
}

// GetUserAutoGroup 根据用户分组获取自动分组设置
func GetUserAutoGroup(userGroup string, userIDs ...int) []string {
	autoGroups := make([]string, 0)
	seen := make(map[string]struct{})
	for _, group := range setting.GetAutoGroups() {
		if !IsUserSelectableGroup(userGroup, group, userIDs...) {
			continue
		}
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		autoGroups = append(autoGroups, group)
	}
	return autoGroups
}

// FilterUserTokenAutoGroups applies current permissions before the current
// per-token limit. It intentionally does not fall back to the global Auto list.
func FilterUserTokenAutoGroups(userGroup string, groups []string, userIDs ...int) []string {
	maxCount := setting.GetMaxTokenAutoGroups()
	filtered := make([]string, 0, min(len(groups), maxCount))
	seen := make(map[string]struct{})
	for _, group := range groups {
		if !IsUserSelectableGroup(userGroup, group, userIDs...) {
			continue
		}
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		filtered = append(filtered, group)
		if len(filtered) == maxCount {
			break
		}
	}
	return filtered
}

// GetRequestAutoGroups resolves the ordered Auto groups for the current token.
// The absence of the context value means that the token inherits the complete
// global Auto list; a present (even empty) value is an explicit token snapshot.
func GetRequestAutoGroups(c *gin.Context, userGroup string) []string {
	value, ok := common.GetContextKey(c, constant.ContextKeyTokenAutoGroups)
	if !ok {
		return GetUserAutoGroup(userGroup, common.GetContextKeyInt(c, constant.ContextKeyUserId))
	}
	groups, ok := value.([]string)
	if !ok {
		return []string{}
	}
	return FilterUserTokenAutoGroups(userGroup, groups, common.GetContextKeyInt(c, constant.ContextKeyUserId))
}

// GetGroupsEnabledModels 按 groups 顺序获取各分组启用的模型并去重
func GetGroupsEnabledModels(groups []string) []string {
	seen := make(map[string]struct{})
	models := make([]string, 0)
	for _, group := range groups {
		for _, modelName := range model.GetGroupEnabledModels(group) {
			if _, ok := seen[modelName]; !ok {
				seen[modelName] = struct{}{}
				models = append(models, modelName)
			}
		}
	}
	return models
}

// GetUserGroupRatio 获取用户使用某个分组的实际生效倍率(与计费一致:
// 含按用户组特殊倍率、按用户 ID 专属倍率与当前限时窗口)。
// userGroup 用户分组;group 需要获取倍率的分组;userId 用户 ID(<=0 时跳过用户级)。
func GetUserGroupRatio(userGroup, group string, userIDs ...int) float64 {
	userId := 0
	if len(userIDs) > 0 {
		userId = userIDs[0]
	}
	return ratio_setting.ResolveGroupRatio(userId, userGroup, group, common.GetTimestamp()).GroupRatio
}
