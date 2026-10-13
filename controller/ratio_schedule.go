package controller

import (
	"net/http"
	"strconv"

	"new-router/common"
	"new-router/model"
	"new-router/service"
	"new-router/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

// GetRatioSchedules 列出全部限时倍率窗口。
func GetRatioSchedules(c *gin.Context) {
	schedules, err := model.GetAllRatioSchedules()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    schedules,
	})
}

type ratioScheduleRequest struct {
	GroupName          string  `json:"group_name"`
	StartMinute        int     `json:"start_minute"`
	EndMinute          int     `json:"end_minute"`
	Mode               string  `json:"mode"`
	TargetRatio        float64 `json:"target_ratio"`
	StackWithUserRatio bool    `json:"stack_with_user_ratio"`
	Enabled            bool    `json:"enabled"`
}

func ratioScheduleBadRequest(c *gin.Context, message string) {
	c.JSON(http.StatusOK, gin.H{
		"success": false,
		"message": message,
	})
}

// AddRatioSchedule 新增一条限时倍率窗口。窗口内在价格计算时生效,窗口外自动恢复。
func AddRatioSchedule(c *gin.Context) {
	var req ratioScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.GroupName == "" {
		ratioScheduleBadRequest(c, "分组名不能为空")
		return
	}
	if !ratio_setting.ContainsGroupRatio(req.GroupName) {
		ratioScheduleBadRequest(c, "分组不存在: "+req.GroupName)
		return
	}
	if req.StartMinute < 0 || req.StartMinute > 1439 || req.EndMinute < 0 || req.EndMinute > 1439 {
		ratioScheduleBadRequest(c, "时间必须在 00:00-23:59 之间")
		return
	}
	if req.StartMinute == req.EndMinute {
		ratioScheduleBadRequest(c, "开始与结束时间不能相同")
		return
	}
	if req.Mode == "" {
		req.Mode = model.RatioScheduleModeMultiply
	}
	if req.Mode != model.RatioScheduleModeOverride && req.Mode != model.RatioScheduleModeMultiply {
		ratioScheduleBadRequest(c, "不支持的模式: "+req.Mode)
		return
	}
	if req.TargetRatio < 0 {
		ratioScheduleBadRequest(c, "倍率不能为负")
		return
	}

	schedule := &model.RatioSchedule{
		GroupName:          req.GroupName,
		StartMinute:        req.StartMinute,
		EndMinute:          req.EndMinute,
		Mode:               req.Mode,
		TargetRatio:        req.TargetRatio,
		StackWithUserRatio: req.StackWithUserRatio,
		Enabled:            req.Enabled,
	}
	if err := model.CreateRatioSchedule(schedule); err != nil {
		common.ApiError(c, err)
		return
	}
	// 立即刷新本地缓存,把生效延迟从「最长一个 tick」降到即时;其他节点靠自身 tick 收敛。
	service.ReloadRatioScheduleCache()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    schedule,
	})
}

// DeleteRatioSchedule 删除指定限时倍率窗口。
func DeleteRatioSchedule(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.DeleteRatioSchedule(id); err != nil {
		common.ApiError(c, err)
		return
	}
	service.ReloadRatioScheduleCache()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}
