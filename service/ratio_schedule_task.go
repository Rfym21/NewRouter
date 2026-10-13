package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"new-router/logger"
	"new-router/model"
	"new-router/setting/ratio_setting"

	"github.com/bytedance/gopkg/util/gopool"
)

const ratioScheduleTickInterval = 30 * time.Second

var (
	ratioScheduleOnce    sync.Once
	ratioScheduleRunning atomic.Bool
)

// StartRatioScheduleTask 启动限时倍率窗口的缓存刷新循环。
// 与旧实现不同,此循环在【每个节点】都必须运行:定时倍率已改为价格计算时求值,
// 各节点依赖本地 scheduleCache,若仅主节点刷新会导致从节点定价与主节点不一致。
func StartRatioScheduleTask() {
	ratioScheduleOnce.Do(func() {
		gopool.Go(func() {
			logger.LogInfo(context.Background(), fmt.Sprintf("ratio schedule cache refresher started: tick=%s", ratioScheduleTickInterval))
			ticker := time.NewTicker(ratioScheduleTickInterval)
			defer ticker.Stop()

			ReloadRatioScheduleCache()
			for range ticker.C {
				ReloadRatioScheduleCache()
			}
		})
	})
}

// ReloadRatioScheduleCache 从数据库拉取全部窗口并整体刷入 ratio_setting 缓存。
// controller 在增删窗口后可同步调用一次,把生效延迟从「最长一个 tick」降到即时。
func ReloadRatioScheduleCache() {
	if !ratioScheduleRunning.CompareAndSwap(false, true) {
		return
	}
	defer ratioScheduleRunning.Store(false)
	defer func() {
		if r := recover(); r != nil {
			logger.LogWarn(context.Background(), fmt.Sprintf("ratio schedule cache refresh panic: %v", r))
		}
	}()

	schedules, err := model.GetAllRatioSchedules()
	if err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("ratio schedule query failed: %v", err))
		return
	}

	windows := make([]ratio_setting.ScheduleWindow, 0, len(schedules))
	for _, s := range schedules {
		windows = append(windows, ratio_setting.ScheduleWindow{
			Id:                 s.Id,
			GroupName:          s.GroupName,
			StartMinute:        s.StartMinute,
			EndMinute:          s.EndMinute,
			Mode:               s.Mode,
			TargetValue:        s.TargetRatio,
			StackWithUserRatio: s.StackWithUserRatio,
			Enabled:            s.Enabled,
		})
	}
	ratio_setting.RefreshScheduleCache(windows)
}
