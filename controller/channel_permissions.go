package controller

import (
	"new-router/common"
	"new-router/model"

	"github.com/gin-gonic/gin"
)

// 渠道标签白名单裁剪:普通管理员(role==10)仅能管理被授权标签下的渠道。白名单由鉴权中间件
// (middleware.authHelper)仅对普通管理员注入到 gin 上下文;root 与普通用户不注入,视为不受限。
// 该维度与 authz 权限点(动作维度)正交:authz 控制「能否执行某类渠道操作」,本文件控制
// 「能操作哪些具体渠道」。白名单为空同样表示不限(向后兼容存量管理员)。

// GetAllChannelTags 返回全部去重、非空的渠道标签,供超管为普通管理员配置可管理标签白名单时选择。
func GetAllChannelTags(c *gin.Context) {
	tags, err := model.GetAllDistinctChannelTags()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, tags)
}

// getAdminManagedTags 返回当前登录管理员的可管理渠道标签白名单。
// 未注入(root / 普通用户 / 非管理接口)时返回 nil,表示不受限。
func getAdminManagedTags(c *gin.Context) []string {
	v, ok := c.Get("admin_managed_tags")
	if !ok {
		return nil
	}
	tags, _ := v.([]string)
	return tags
}

// tagInManagedSet 报告 tag 是否落在白名单集合内。
func tagInManagedSet(managed []string, tag string) bool {
	for _, t := range managed {
		if t == tag {
			return true
		}
	}
	return false
}

// assertChannelTagAllowed 校验某渠道是否在当前管理员可管理的标签范围内。白名单为空直接放行;
// 越权时写出错误响应并返回 false。传入已加载的 channel 以复用查询结果。
func assertChannelTagAllowed(c *gin.Context, channel *model.Channel) bool {
	managed := getAdminManagedTags(c)
	if len(managed) == 0 {
		return true
	}
	if channel != nil && tagInManagedSet(managed, channel.GetTag()) {
		return true
	}
	common.ApiErrorMsg(c, "无权管理该渠道:不在您可管理的标签范围内")
	return false
}

// assertChannelIDManageable 按渠道 ID 加载渠道并校验归属。白名单为空直接放行。
func assertChannelIDManageable(c *gin.Context, channelID int) bool {
	if len(getAdminManagedTags(c)) == 0 {
		return true
	}
	channel, err := model.GetChannelById(channelID, false)
	if err != nil {
		common.ApiError(c, err)
		return false
	}
	return assertChannelTagAllowed(c, channel)
}

// assertChannelIDsManageable 批量校验渠道 ID 归属,任一越界即拒绝。用于批量删除/改标签等操作。
func assertChannelIDsManageable(c *gin.Context, channelIDs []int) bool {
	managed := getAdminManagedTags(c)
	if len(managed) == 0 {
		return true
	}
	for _, id := range channelIDs {
		channel, err := model.GetChannelById(id, false)
		if err != nil {
			common.ApiError(c, err)
			return false
		}
		if !tagInManagedSet(managed, channel.GetTag()) {
			common.ApiErrorMsg(c, "存在超出您可管理标签范围的渠道")
			return false
		}
	}
	return true
}

// assertTagManageable 校验某标签本身是否在白名单内。用于直接对某 tag 的操作,或新建渠道时的目标 tag。
func assertTagManageable(c *gin.Context, tag string) bool {
	managed := getAdminManagedTags(c)
	if len(managed) == 0 {
		return true
	}
	if tagInManagedSet(managed, tag) {
		return true
	}
	common.ApiErrorMsg(c, "无权管理该标签:不在您可管理的标签范围内")
	return false
}

// assertNotTagRestricted 用于「影响全部渠道」的危险操作(如批量测试/删除全部禁用渠道):
// 仅不受限(白名单为空)的管理员放行,受限管理员一律拒绝。
func assertNotTagRestricted(c *gin.Context) bool {
	if len(getAdminManagedTags(c)) == 0 {
		return true
	}
	common.ApiErrorMsg(c, "您的渠道管理范围受标签限制,无法执行影响全部渠道的操作")
	return false
}
