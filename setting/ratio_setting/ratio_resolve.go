package ratio_setting

import (
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"time"
)

// 倍率模式:override 为绝对覆盖(固定值),multiply 为在基准倍率上乘以折扣系数。
const (
	RatioModeOverride = "override"
	RatioModeMultiply = "multiply"
)

// RatioEntry 是一条可携带模式的倍率条目,用于「按用户组」与「按用户 ID」的专属倍率。
// 为兼容历史数据(内层值曾是裸 float64),UnmarshalJSON 接受裸数字并视作 override,
// MarshalJSON 在 override 模式下退化输出裸数字,使旧前端与多节点同步无感知。
type RatioEntry struct {
	Mode  string  `json:"mode"`
	Value float64 `json:"value"`
}

// UnmarshalJSON 兼容两种形态:裸数字(旧数据,等价 override)与 {mode,value} 对象。
func (e *RatioEntry) UnmarshalJSON(b []byte) error {
	var num float64
	if err := json.Unmarshal(b, &num); err == nil {
		e.Mode = RatioModeOverride
		e.Value = num
		return nil
	}
	// 用别名类型避免递归调用本方法。
	type ratioEntryAlias RatioEntry
	var alias ratioEntryAlias
	if err := json.Unmarshal(b, &alias); err != nil {
		return err
	}
	e.Mode = alias.Mode
	e.Value = alias.Value
	if e.Mode == "" {
		e.Mode = RatioModeOverride
	}
	return nil
}

// MarshalJSON 在 override 模式退化为裸数字以保持向后兼容,multiply 才输出对象。
func (e RatioEntry) MarshalJSON() ([]byte, error) {
	if e.Mode == "" || e.Mode == RatioModeOverride {
		return json.Marshal(e.Value)
	}
	type ratioEntryAlias RatioEntry
	return json.Marshal(ratioEntryAlias(e))
}

// Resolve 依据模式在给定基准倍率上求出等效倍率:override 直接取 Value;
// multiply 取 base*Value(base 即「该用户此前应生效的倍率」)。
func (e RatioEntry) Resolve(base float64) float64 {
	if e.Mode == RatioModeMultiply {
		return base * e.Value
	}
	return e.Value
}

// ScheduleWindow 是一条每日限时倍率窗口(定时倍率调整的运行时形态)。
// 窗口以本地时区的「一天内分钟数」[StartMinute,EndMinute) 表示,支持跨午夜。
// 窗口内在价格计算时叠加/覆盖倍率,窗口外自动恢复,不再永久改写 GroupRatio。
type ScheduleWindow struct {
	Id                 int
	GroupName          string
	StartMinute        int
	EndMinute          int
	Mode               string
	TargetValue        float64
	StackWithUserRatio bool
	Enabled            bool
}

// IsActiveAt 判断窗口在 now(Unix 秒)对应的本地时刻是否激活。
// StartMinute<EndMinute 为同日窗口 [start,end);start>end 为跨午夜 [start,1440)∪[0,end);
// 二者相等视为退化窗口,恒不激活。
func (w ScheduleWindow) IsActiveAt(now int64) bool {
	if !w.Enabled || w.StartMinute == w.EndMinute {
		return false
	}
	m := minuteOfDayLocal(now)
	if w.StartMinute < w.EndMinute {
		return m >= w.StartMinute && m < w.EndMinute
	}
	return m >= w.StartMinute || m < w.EndMinute
}

func minuteOfDayLocal(now int64) int {
	t := time.Unix(now, 0).Local()
	return t.Hour()*60 + t.Minute()
}

// scheduleCache 按分组名分桶缓存限时窗口,供价格计算热路径读取。
// 由 service 层定时任务把 model 行转换后整体推入(RefreshScheduleCache),
// 保持 ratio_setting 不反向依赖 model,避免导入环。
var scheduleCache = struct {
	sync.RWMutex
	byGroup map[string][]ScheduleWindow
}{byGroup: map[string][]ScheduleWindow{}}

// RefreshScheduleCache 用最新窗口集整体替换缓存。必须在每个节点周期性调用,
// 否则从节点缓存为空会导致定价与主节点不一致。
func RefreshScheduleCache(windows []ScheduleWindow) {
	byGroup := make(map[string][]ScheduleWindow, len(windows))
	for _, w := range windows {
		byGroup[w.GroupName] = append(byGroup[w.GroupName], w)
	}
	scheduleCache.Lock()
	scheduleCache.byGroup = byGroup
	scheduleCache.Unlock()
}

func activeWindows(group string, now int64) []ScheduleWindow {
	scheduleCache.RLock()
	windows := scheduleCache.byGroup[group]
	scheduleCache.RUnlock()
	if len(windows) == 0 {
		return nil
	}
	active := make([]ScheduleWindow, 0, len(windows))
	for _, w := range windows {
		if w.IsActiveAt(now) {
			active = append(active, w)
		}
	}
	return active
}

// applyWindow 计算单个窗口作用后的倍率。
// override 绝对覆盖(忽略叠加开关);multiply 且叠加开启时作用于已解析的专属倍率,
// 关闭时作用于基础分组倍率(base)。
func applyWindow(w ScheduleWindow, base, resolved float64) float64 {
	if w.Mode == RatioModeOverride {
		return w.TargetValue
	}
	if w.StackWithUserRatio {
		return resolved * w.TargetValue
	}
	return base * w.TargetValue
}

// GroupRatioResolution 是一次组倍率解析的完整结果,GroupRatio 为最终计费值,
// 其余字段仅用于日志/观测。
type GroupRatioResolution struct {
	GroupRatio      float64
	SpecialRatio    float64 // 命中的专属倍率(已解析等效值),未命中为 -1
	HasSpecialRatio bool
	HasUserRatio    bool // 命中了按用户 ID 的专属倍率
	HasSchedule     bool // 命中了限时窗口
}

// ResolveGroupRatio 是组倍率的唯一解析入口,统一 relay 主链、realtime 与 task 计费。
// 级联顺序:基础分组倍率 → 按用户组特殊倍率 → 按用户 ID 专属倍率 → 限时窗口。
// now 为 Unix 秒(用于限时窗口判定),userId<=0 或空分组时相应层自动跳过。
func ResolveGroupRatio(userId int, userGroup, usingGroup string, now int64) GroupRatioResolution {
	r0 := GetGroupRatio(usingGroup)
	res := GroupRatioResolution{GroupRatio: r0, SpecialRatio: -1}

	r := r0
	if entry, ok := GetGroupGroupRatioEntry(userGroup, usingGroup); ok {
		r = entry.Resolve(r)
		res.HasSpecialRatio = true
		res.SpecialRatio = r
	}
	if entry, ok := GetUserGroupRatioEntry(userId, usingGroup); ok {
		r = entry.Resolve(r)
		res.HasSpecialRatio = true
		res.HasUserRatio = true
		res.SpecialRatio = r
	}

	if active := activeWindows(usingGroup, now); len(active) > 0 {
		res.HasSchedule = true
		best := math.Inf(1)
		bestId := 0
		for _, w := range active {
			cand := applyWindow(w, r0, r)
			if bestId == 0 || cand < best || (cand == best && w.Id < bestId) {
				best = cand
				bestId = w.Id
			}
		}
		r = best
	}

	res.GroupRatio = sanitizeRatio(r, r0)
	return res
}

// sanitizeRatio 清洗最终倍率:NaN/Inf 回退基础倍率,负值归零,
// 并 round 到 1e-9 消除 0.8*0.8=0.6400000000000001 之类浮点尾巴。
func sanitizeRatio(r, fallback float64) float64 {
	if math.IsNaN(r) || math.IsInf(r, 0) {
		return fallback
	}
	if r <= 0 {
		return 0
	}
	rounded := math.Round(r*1e9) / 1e9
	if rounded <= 0 {
		return 0
	}
	return rounded
}

// validateRatioMode 校验倍率模式取值。
func validateRatioMode(mode string) error {
	switch mode {
	case "", RatioModeOverride, RatioModeMultiply:
		return nil
	default:
		return fmt.Errorf("invalid ratio mode: %s", mode)
	}
}
