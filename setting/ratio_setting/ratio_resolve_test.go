package ratio_setting

import (
	"testing"
	"time"
)

// localNow 构造「今天本地时区 hh:mm」对应的 Unix 秒,用于窗口激活的确定性测试。
func localNow(hh, mm int) int64 {
	now := time.Now().Local()
	t := time.Date(now.Year(), now.Month(), now.Day(), hh, mm, 0, 0, time.Local)
	return t.Unix()
}

// resetRatioState 清空三套倍率 map 与限时窗口缓存,保证用例互不污染。
func resetRatioState(t *testing.T) {
	t.Helper()
	groupRatioMap.Clear()
	groupGroupRatioMap.Clear()
	userGroupRatioMap.Clear()
	RefreshScheduleCache(nil)
}

func TestRatioEntryResolve(t *testing.T) {
	cases := []struct {
		name  string
		entry RatioEntry
		base  float64
		want  float64
	}{
		{"override 忽略 base", RatioEntry{Mode: RatioModeOverride, Value: 0.5}, 2, 0.5},
		{"multiply 基 1", RatioEntry{Mode: RatioModeMultiply, Value: 0.8}, 1, 0.8},
		{"multiply 基 2", RatioEntry{Mode: RatioModeMultiply, Value: 0.8}, 2, 1.6},
		{"空模式视作 override", RatioEntry{Value: 0.3}, 5, 0.3},
	}
	for _, c := range cases {
		if got := c.entry.Resolve(c.base); got != c.want {
			t.Errorf("%s: Resolve(%v)=%v want %v", c.name, c.base, got, c.want)
		}
	}
}

func TestResolveGroupRatio(t *testing.T) {
	const eps = 1e-9
	// 激活窗口用「当前本地时刻」保证 IsActiveAt 命中;不激活用远离当前的窗口。
	now := time.Now().Local()
	curMin := now.Hour()*60 + now.Minute()

	type setup struct {
		groupRatio      map[string]float64
		groupGroupRatio map[string]map[string]RatioEntry
		userRatio       map[string]map[string]RatioEntry
		windows         []ScheduleWindow
	}
	cases := []struct {
		name    string
		setup   setup
		userId  int
		userGrp string
		useGrp  string
		now     int64
		want    float64
	}{
		{
			name:   "D1 专属 override 0.5",
			setup:  setup{groupRatio: map[string]float64{"default": 1}, userRatio: map[string]map[string]RatioEntry{"7": {"default": {Mode: RatioModeOverride, Value: 0.5}}}},
			userId: 7, userGrp: "default", useGrp: "default", now: localNow(12, 0), want: 0.5,
		},
		{
			name:   "D1 专属 multiply 0.8 基 1",
			setup:  setup{groupRatio: map[string]float64{"default": 1}, userRatio: map[string]map[string]RatioEntry{"7": {"default": {Mode: RatioModeMultiply, Value: 0.8}}}},
			userId: 7, userGrp: "default", useGrp: "default", now: localNow(12, 0), want: 0.8,
		},
		{
			name:   "D1 专属 multiply 0.8 基 2",
			setup:  setup{groupRatio: map[string]float64{"vip": 2}, userRatio: map[string]map[string]RatioEntry{"7": {"vip": {Mode: RatioModeMultiply, Value: 0.8}}}},
			userId: 7, userGrp: "vip", useGrp: "vip", now: localNow(12, 0), want: 1.6,
		},
		{
			name: "D3 窗口 multiply+Stack:专属0.8×限时0.8=0.64",
			setup: setup{
				groupRatio: map[string]float64{"default": 1},
				userRatio:  map[string]map[string]RatioEntry{"7": {"default": {Mode: RatioModeMultiply, Value: 0.8}}},
				windows:    []ScheduleWindow{{Id: 1, GroupName: "default", StartMinute: curMin - 1, EndMinute: curMin + 1, Mode: RatioModeMultiply, TargetValue: 0.8, StackWithUserRatio: true, Enabled: true}},
			},
			userId: 7, userGrp: "default", useGrp: "default", now: now.Unix(), want: 0.64,
		},
		{
			name: "D3 窗口 multiply+!Stack:忽略专属,基1×0.8=0.8",
			setup: setup{
				groupRatio: map[string]float64{"default": 1},
				userRatio:  map[string]map[string]RatioEntry{"7": {"default": {Mode: RatioModeMultiply, Value: 0.8}}},
				windows:    []ScheduleWindow{{Id: 1, GroupName: "default", StartMinute: curMin - 1, EndMinute: curMin + 1, Mode: RatioModeMultiply, TargetValue: 0.8, StackWithUserRatio: false, Enabled: true}},
			},
			userId: 7, userGrp: "default", useGrp: "default", now: now.Unix(), want: 0.8,
		},
		{
			name: "窗口 override 忽略 stack 与专属",
			setup: setup{
				groupRatio: map[string]float64{"default": 1},
				userRatio:  map[string]map[string]RatioEntry{"7": {"default": {Mode: RatioModeMultiply, Value: 0.8}}},
				windows:    []ScheduleWindow{{Id: 1, GroupName: "default", StartMinute: curMin - 1, EndMinute: curMin + 1, Mode: RatioModeOverride, TargetValue: 0.3, StackWithUserRatio: true, Enabled: true}},
			},
			userId: 7, userGrp: "default", useGrp: "default", now: now.Unix(), want: 0.3,
		},
		{
			name: "两窗口重叠取最低价",
			setup: setup{
				groupRatio: map[string]float64{"default": 1},
				windows: []ScheduleWindow{
					{Id: 1, GroupName: "default", StartMinute: curMin - 1, EndMinute: curMin + 1, Mode: RatioModeMultiply, TargetValue: 0.9, Enabled: true},
					{Id: 2, GroupName: "default", StartMinute: curMin - 1, EndMinute: curMin + 1, Mode: RatioModeMultiply, TargetValue: 0.5, Enabled: true},
				},
			},
			userId: 0, userGrp: "default", useGrp: "default", now: now.Unix(), want: 0.5,
		},
		{
			name: "用户组特殊倍率作为专属基数(D1)",
			setup: setup{
				groupRatio:      map[string]float64{"default": 1},
				groupGroupRatio: map[string]map[string]RatioEntry{"vip": {"default": {Mode: RatioModeOverride, Value: 0.5}}},
				userRatio:       map[string]map[string]RatioEntry{"7": {"default": {Mode: RatioModeMultiply, Value: 0.8}}},
			},
			userId: 7, userGrp: "vip", useGrp: "default", now: localNow(12, 0), want: 0.4, // 0.5(用户组) × 0.8(专属折扣)
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resetRatioState(t)
			for k, v := range c.setup.groupRatio {
				groupRatioMap.Set(k, v)
			}
			for k, v := range c.setup.groupGroupRatio {
				groupGroupRatioMap.Set(k, v)
			}
			for k, v := range c.setup.userRatio {
				userGroupRatioMap.Set(k, v)
			}
			RefreshScheduleCache(c.setup.windows)

			got := ResolveGroupRatio(c.userId, c.userGrp, c.useGrp, c.now).GroupRatio
			if diff := got - c.want; diff > eps || diff < -eps {
				t.Errorf("ResolveGroupRatio=%v want %v", got, c.want)
			}
		})
	}
}

func TestScheduleWindowIsActiveAt(t *testing.T) {
	// 跨午夜窗口 22:00-02:00(1320-120)
	w := ScheduleWindow{StartMinute: 1320, EndMinute: 120, Mode: RatioModeMultiply, Enabled: true}
	cases := []struct {
		hh, mm int
		want   bool
	}{
		{23, 0, true},  // 窗口内(午夜前)
		{1, 0, true},   // 窗口内(午夜后)
		{12, 0, false}, // 窗口外
		{22, 0, true},  // 起点含
		{2, 0, false},  // 终点不含
	}
	for _, c := range cases {
		if got := w.IsActiveAt(localNow(c.hh, c.mm)); got != c.want {
			t.Errorf("cross-midnight IsActiveAt(%02d:%02d)=%v want %v", c.hh, c.mm, got, c.want)
		}
	}

	// 禁用窗口恒不激活
	disabled := ScheduleWindow{StartMinute: 0, EndMinute: 1439, Enabled: false}
	if disabled.IsActiveAt(localNow(12, 0)) {
		t.Error("disabled window should never be active")
	}
}

func TestRatioEntryJSONBackwardCompat(t *testing.T) {
	// 裸数字应解析为 override
	var e RatioEntry
	if err := e.UnmarshalJSON([]byte("0.75")); err != nil {
		t.Fatal(err)
	}
	if e.Mode != RatioModeOverride || e.Value != 0.75 {
		t.Errorf("bare number decode = %+v", e)
	}
	// override 应序列化回裸数字
	b, err := e.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "0.75" {
		t.Errorf("override marshal = %s want 0.75", b)
	}
	// multiply 应序列化为对象
	m := RatioEntry{Mode: RatioModeMultiply, Value: 0.8}
	b, _ = m.MarshalJSON()
	var round RatioEntry
	if err := round.UnmarshalJSON(b); err != nil {
		t.Fatal(err)
	}
	if round.Mode != RatioModeMultiply || round.Value != 0.8 {
		t.Errorf("multiply round-trip = %+v (json=%s)", round, b)
	}
}
