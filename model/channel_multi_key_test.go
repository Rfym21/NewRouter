package model

import (
	"strings"
	"testing"

	"new-router/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// indexSet 把用例里的索引切片转成 RemoveMultiKeys 需要的集合。
func indexSet(indexes []int) map[int]struct{} {
	set := make(map[int]struct{}, len(indexes))
	for _, idx := range indexes {
		set[idx] = struct{}{}
	}
	return set
}

// newRemoveFixture 构造 5 个 key 的多密钥渠道：
// 索引 0/3 手动禁用、2 自动禁用、1 显式启用、4 无状态条目（隐式启用）。
// Keys 故意预填成 gorm:"-" 的瞬态缓存，用来钉住 RemoveMultiKeys 必须清空它这一点：
// 少了那一行，GetKeys() 会短路返回删除前的旧列表。
func newRemoveFixture() *Channel {
	return &Channel{
		Id:   1,
		Key:  strings.Join([]string{"k0", "k1", "k2", "k3", "k4"}, "\n"),
		Keys: []string{"k0", "k1", "k2", "k3", "k4"},
		ChannelInfo: ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 5,
			MultiKeyStatusList: map[int]int{
				0: common.ChannelStatusManuallyDisabled,
				1: common.ChannelStatusEnabled,
				2: common.ChannelStatusAutoDisabled,
				3: common.ChannelStatusManuallyDisabled,
			},
			MultiKeyDisabledReason: map[int]string{0: "manual", 2: "upstream 401", 3: "m4"},
			MultiKeyDisabledTime:   map[int]int64{0: 101, 2: 103, 3: 104},
		},
	}
}

// TestRemoveMultiKeys 钉住删除后的重编号契约。
// 这个重编号正是前端必须在任何 delete 成功后丢弃 index-keyed 测试结果与 RowSelectionState 的原因：
// 存活密钥的编号会整体前移，旧 index 指向的密钥与删除前不再是同一个。
func TestRemoveMultiKeys(t *testing.T) {
	cases := []struct {
		name string
		// nilMaps 把三张 map 全置 nil（渠道从未有过 per-key 状态的情形）
		nilMaps bool
		// orphan 追加一条没有对应 status 条目的 reason/time（Update() 裁剪循环留下的孤儿）
		orphan      bool
		deleted     []int
		wantRemoved int
		wantKey     string
		wantKeys    []string
		wantSize    int
		wantStatus  map[int]int
		wantReason  map[int]string
		wantTime    map[int]int64
	}{
		{
			name:        "a 空集合不删除任何密钥",
			deleted:     nil,
			wantRemoved: 0,
			wantKey:     "k0\nk1\nk2\nk3\nk4",
			wantKeys:    []string{"k0", "k1", "k2", "k3", "k4"},
			wantSize:    5,
			// 索引 1 显式为 enabled，按统一语义不再携带，其余条目原位保留
			wantStatus: map[int]int{0: common.ChannelStatusManuallyDisabled, 2: common.ChannelStatusAutoDisabled, 3: common.ChannelStatusManuallyDisabled},
			wantReason: map[int]string{0: "manual", 2: "upstream 401", 3: "m4"},
			wantTime:   map[int]int64{0: 101, 2: 103, 3: 104},
		},
		{
			name:        "b 删除首个索引",
			deleted:     []int{0},
			wantRemoved: 1,
			wantKey:     "k1\nk2\nk3\nk4",
			wantKeys:    []string{"k1", "k2", "k3", "k4"},
			wantSize:    4,
			wantStatus:  map[int]int{1: common.ChannelStatusAutoDisabled, 2: common.ChannelStatusManuallyDisabled},
			wantReason:  map[int]string{1: "upstream 401", 2: "m4"},
			wantTime:    map[int]int64{1: 103, 2: 104},
		},
		{
			name:        "c 删除首尾之间的两个索引",
			deleted:     []int{0, 3},
			wantRemoved: 2,
			wantKey:     "k1\nk2\nk4",
			wantKeys:    []string{"k1", "k2", "k4"},
			wantSize:    3,
			wantStatus:  map[int]int{1: common.ChannelStatusAutoDisabled},
			wantReason:  map[int]string{1: "upstream 401"},
			wantTime:    map[int]int64{1: 103},
		},
		{
			name:        "d 删除显式 enabled 的索引",
			deleted:     []int{1},
			wantRemoved: 1,
			wantKey:     "k0\nk2\nk3\nk4",
			wantKeys:    []string{"k0", "k2", "k3", "k4"},
			wantSize:    4,
			wantStatus:  map[int]int{0: common.ChannelStatusManuallyDisabled, 1: common.ChannelStatusAutoDisabled, 2: common.ChannelStatusManuallyDisabled},
			wantReason:  map[int]string{0: "manual", 1: "upstream 401", 2: "m4"},
			wantTime:    map[int]int64{0: 101, 1: 103, 2: 104},
		},
		{
			name:        "e1 删除最后一个索引",
			deleted:     []int{4},
			wantRemoved: 1,
			wantKey:     "k0\nk1\nk2\nk3",
			wantKeys:    []string{"k0", "k1", "k2", "k3"},
			wantSize:    4,
			wantStatus:  map[int]int{0: common.ChannelStatusManuallyDisabled, 2: common.ChannelStatusAutoDisabled, 3: common.ChannelStatusManuallyDisabled},
			wantReason:  map[int]string{0: "manual", 2: "upstream 401", 3: "m4"},
			wantTime:    map[int]int64{0: 101, 2: 103, 3: 104},
		},
		{
			name:        "e2 删除连续块",
			deleted:     []int{1, 2, 3},
			wantRemoved: 3,
			wantKey:     "k0\nk4",
			wantKeys:    []string{"k0", "k4"},
			wantSize:    2,
			wantStatus:  map[int]int{0: common.ChannelStatusManuallyDisabled},
			wantReason:  map[int]string{0: "manual"},
			wantTime:    map[int]int64{0: 101},
		},
		{
			name:        "e3 删除非连续索引",
			deleted:     []int{0, 2, 4},
			wantRemoved: 3,
			wantKey:     "k1\nk3",
			wantKeys:    []string{"k1", "k3"},
			wantSize:    2,
			wantStatus:  map[int]int{1: common.ChannelStatusManuallyDisabled},
			wantReason:  map[int]string{1: "m4"},
			wantTime:    map[int]int64{1: 104},
		},
		{
			name:        "f 三张 map 全为 nil",
			nilMaps:     true,
			deleted:     []int{0},
			wantRemoved: 1,
			wantKey:     "k1\nk2\nk3\nk4",
			wantKeys:    []string{"k1", "k2", "k3", "k4"},
			wantSize:    4,
			wantStatus:  map[int]int{},
			wantReason:  map[int]string{},
			wantTime:    map[int]int64{},
		},
		{
			name:        "g 删除全部索引",
			deleted:     []int{0, 1, 2, 3, 4},
			wantRemoved: 5,
			wantKey:     "",
			wantKeys:    []string{},
			wantSize:    0,
			wantStatus:  map[int]int{},
			wantReason:  map[int]string{},
			wantTime:    map[int]int64{},
		},
		{
			name:        "h 丢弃没有 status 条目的孤儿 reason/time",
			orphan:      true,
			deleted:     nil,
			wantRemoved: 0,
			wantKey:     "k0\nk1\nk2\nk3\nk4",
			wantKeys:    []string{"k0", "k1", "k2", "k3", "k4"},
			wantSize:    5,
			wantStatus:  map[int]int{0: common.ChannelStatusManuallyDisabled, 2: common.ChannelStatusAutoDisabled, 3: common.ChannelStatusManuallyDisabled},
			wantReason:  map[int]string{0: "manual", 2: "upstream 401", 3: "m4"},
			wantTime:    map[int]int64{0: 101, 2: 103, 3: 104},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			channel := newRemoveFixture()
			if c.nilMaps {
				channel.ChannelInfo.MultiKeyStatusList = nil
				channel.ChannelInfo.MultiKeyDisabledReason = nil
				channel.ChannelInfo.MultiKeyDisabledTime = nil
			}
			if c.orphan {
				channel.ChannelInfo.MultiKeyDisabledReason[4] = "orphan"
				channel.ChannelInfo.MultiKeyDisabledTime[4] = 999
			}

			removed := channel.RemoveMultiKeys(indexSet(c.deleted))

			assert.Equal(t, c.wantRemoved, removed)
			assert.Equal(t, c.wantKey, channel.Key)
			assert.Equal(t, c.wantSize, channel.ChannelInfo.MultiKeySize)
			assert.Equal(t, c.wantStatus, channel.ChannelInfo.MultiKeyStatusList)
			assert.Equal(t, c.wantReason, channel.ChannelInfo.MultiKeyDisabledReason)
			assert.Equal(t, c.wantTime, channel.ChannelInfo.MultiKeyDisabledTime)
			// 瞬态缓存必须清空，否则 GetKeys() 会短路返回删除前的列表
			assert.Nil(t, channel.Keys)
			assert.Equal(t, c.wantKeys, channel.GetKeys())
			// 存活者的状态表里绝不会留下值为 enabled 的条目
			for idx, status := range channel.ChannelInfo.MultiKeyStatusList {
				assert.NotEqual(t, common.ChannelStatusEnabled, status, "索引 %d 不应携带 enabled 状态", idx)
			}
		})
	}
}

// otherInfoNumber 从 other_info 里取出一个数值字段；GetOtherInfo 走 map[string]any 解码，
// 所以时间戳会以 float64 的形式回来。
func otherInfoNumber(t *testing.T, info map[string]interface{}, key string) int64 {
	t.Helper()
	raw, ok := info[key]
	require.True(t, ok, "other_info 应包含 %s", key)
	value, ok := raw.(float64)
	require.True(t, ok, "%s 应为 JSON number，实际类型 %T", key, raw)
	return int64(value)
}

// TestNormalizeMultiKeyStatus 每个用例都设真实的 newline-joined Key：
// hasEnabledMultiKey 遍历 GetKeys()，而 GetKeys() 在 Key=="" 时返回空切片，
// 只设 MultiKeySize 的 fixture 会让断言得出相反结论。
func TestNormalizeMultiKeyStatus(t *testing.T) {
	cases := []struct {
		name          string
		key           string
		statusList    map[int]int
		status        int
		wantStatus    int
		wantReason    string // 空串 = 期望 status_reason 不存在
		wantFreshTime bool   // status_time 是否应被刷新
	}{
		{
			name:          "a 有可用密钥的自动禁用渠道恢复启用",
			key:           "k0\nk1",
			statusList:    map[int]int{},
			status:        common.ChannelStatusAutoDisabled,
			wantStatus:    common.ChannelStatusEnabled,
			wantReason:    "",
			wantFreshTime: true,
		},
		{
			name:          "b 全部密钥禁用后渠道转为自动禁用",
			key:           "k0\nk1",
			statusList:    map[int]int{0: common.ChannelStatusManuallyDisabled, 1: common.ChannelStatusManuallyDisabled},
			status:        common.ChannelStatusEnabled,
			wantStatus:    common.ChannelStatusAutoDisabled,
			wantReason:    "All keys are disabled",
			wantFreshTime: true,
		},
		{
			name:          "c 手动禁用的渠道不会被密钥级启用复活",
			key:           "k0\nk1",
			statusList:    map[int]int{},
			status:        common.ChannelStatusManuallyDisabled,
			wantStatus:    common.ChannelStatusManuallyDisabled,
			wantReason:    "seeded reason",
			wantFreshTime: false,
		},
		{
			name:          "d 手动禁用的渠道在密钥全禁用时保持手动禁用",
			key:           "k0\nk1",
			statusList:    map[int]int{0: common.ChannelStatusManuallyDisabled, 1: common.ChannelStatusManuallyDisabled},
			status:        common.ChannelStatusManuallyDisabled,
			wantStatus:    common.ChannelStatusManuallyDisabled,
			wantReason:    "seeded reason",
			wantFreshTime: false,
		},
		{
			name:          "e nil 状态表视作全部启用",
			key:           "k0\nk1",
			statusList:    nil,
			status:        common.ChannelStatusAutoDisabled,
			wantStatus:    common.ChannelStatusEnabled,
			wantReason:    "",
			wantFreshTime: true,
		},
	}

	const seededTime = int64(1000)

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			channel := &Channel{
				Id:     7,
				Key:    c.key,
				Status: c.status,
				ChannelInfo: ChannelInfo{
					IsMultiKey:         true,
					MultiKeySize:       len(strings.Split(c.key, "\n")),
					MultiKeyStatusList: c.statusList,
				},
			}
			channel.SetOtherInfo(map[string]interface{}{
				"status_reason": "seeded reason",
				"status_time":   seededTime,
			})

			before := common.GetTimestamp()
			channel.NormalizeMultiKeyStatus()

			assert.Equal(t, c.wantStatus, channel.Status)
			info := channel.GetOtherInfo()
			if c.wantReason == "" {
				_, ok := info["status_reason"]
				assert.False(t, ok, "status_reason 应被清除")
			} else {
				assert.Equal(t, c.wantReason, info["status_reason"])
			}
			statusTime := otherInfoNumber(t, info, "status_time")
			if c.wantFreshTime {
				assert.GreaterOrEqual(t, statusTime, before)
			} else {
				assert.Equal(t, seededTime, statusTime)
			}
		})
	}
}

// TestMultiKeyDisableKeys 钉住"跳过已禁用索引"的语义：auto-disable 写下的上游诊断不能被管理员的
// 批量禁用覆盖，affected 计数也据此驱动 controller 层的 affected==0 分支。
// 名字带 MultiKey 前缀，才能被 `go test ./model/... -run MultiKey` 覆盖到。
func TestMultiKeyDisableKeys(t *testing.T) {
	cases := []struct {
		name string
		// nilMaps 把三张 map 全置 nil，验证惰性分配
		nilMaps      bool
		indexes      []int
		wantAffected int
		wantStatus   map[int]int
		wantReason   map[int]string
		// wantTime 里值为 0 的索引表示"期望被写入新时间戳"，非 0 表示必须逐字节保留
		wantTime map[int]int64
	}{
		{
			name:         "a 只禁用当前为 enabled 的索引",
			indexes:      []int{0, 1},
			wantAffected: 1,
			wantStatus:   map[int]int{0: common.ChannelStatusManuallyDisabled, 1: common.ChannelStatusAutoDisabled},
			wantReason:   map[int]string{0: "manual", 1: "upstream 401"},
			wantTime:     map[int]int64{0: 0, 1: 1000},
		},
		{
			name:         "b 全部请求索引均已禁用",
			indexes:      []int{1},
			wantAffected: 0,
			wantStatus:   map[int]int{1: common.ChannelStatusAutoDisabled},
			wantReason:   map[int]string{1: "upstream 401"},
			wantTime:     map[int]int64{1: 1000},
		},
		{
			name:         "c 三张 map 为 nil 时惰性分配",
			nilMaps:      true,
			indexes:      []int{0},
			wantAffected: 1,
			wantStatus:   map[int]int{0: common.ChannelStatusManuallyDisabled},
			wantReason:   map[int]string{0: "manual"},
			wantTime:     map[int]int64{0: 0},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			info := &ChannelInfo{
				IsMultiKey:             true,
				MultiKeySize:           2,
				MultiKeyStatusList:     map[int]int{1: common.ChannelStatusAutoDisabled},
				MultiKeyDisabledReason: map[int]string{1: "upstream 401"},
				MultiKeyDisabledTime:   map[int]int64{1: 1000},
			}
			if c.nilMaps {
				info.MultiKeyStatusList = nil
				info.MultiKeyDisabledReason = nil
				info.MultiKeyDisabledTime = nil
			}

			before := common.GetTimestamp()
			affected := info.DisableKeys(c.indexes, "manual")

			assert.Equal(t, c.wantAffected, affected)
			assert.Equal(t, c.wantStatus, info.MultiKeyStatusList)
			assert.Equal(t, c.wantReason, info.MultiKeyDisabledReason)
			require.Len(t, info.MultiKeyDisabledTime, len(c.wantTime))
			for idx, want := range c.wantTime {
				got, ok := info.MultiKeyDisabledTime[idx]
				require.True(t, ok, "索引 %d 应存在禁用时间", idx)
				if want == 0 {
					assert.GreaterOrEqual(t, got, before, "索引 %d 应写入新时间戳", idx)
				} else {
					assert.Equal(t, want, got, "索引 %d 的禁用时间应逐字节保留", idx)
				}
			}
		})
	}
}
