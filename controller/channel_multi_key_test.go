package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"new-router/common"
	"new-router/model"
	"new-router/pkg/wsmanager"
	"new-router/service"
	"new-router/service/authz"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// 本文件刻意不写的测试（避免 coverage-only 与 scope creep）：
//   - 单独断言 multiKeyActionRequiresSensitiveWrite("delete_keys") 的测试
//   - 用反射检查 testResult 字段或 upstream_status_code JSON key 是否存在
//   - 随机/模糊 index 数组、1000 个密钥的计时测试
//   - TestChannel 响应形状的 Go 测试（需要真实 adaptor + 上游；契约由
//     web/src/features/channels/types.ts 的 TS 类型与 RemoveMultiKeys 的 index 语义钉住）
//   - types.NewOpenAIError 的 passthrough 测试（上游状态码直接取 httpResp.StatusCode，
//     不依赖 errors.As 行为）

// setupMultiKeyTestDB 装配本仓库第一个 DB-backed 测试所需的最小 harness。
// glebarez/sqlite 是纯 Go 实现（已是直接依赖，无需 CGO）；MaxOpenConns(1) 是必须的，
// 否则连接池里每条新连接都会拿到一个全新的空内存库。
// common.MemoryCacheEnabled 保持 false，让 handler 里的 InitChannelCache() 退化成
// 一次定价缓存失效，无需装配 group2model2channels。
// common.RedisEnabled 默认是 true（只有 InitRedisClient 才会置 false），
// 不显式关掉的话审计写入路径上的 GetUsernameById 会在 nil redis client 上 panic。
func setupMultiKeyTestDB(t *testing.T) {
	t.Helper()
	require.False(t, common.MemoryCacheEnabled, "harness 依赖内存缓存关闭")

	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.User{}, &model.Log{}))

	previousDB, previousLogDB, previousRedis := model.DB, model.LOG_DB, common.RedisEnabled
	model.DB, model.LOG_DB, common.RedisEnabled = db, db, false
	t.Cleanup(func() {
		model.DB, model.LOG_DB, common.RedisEnabled = previousDB, previousLogDB, previousRedis
		_ = sqlDB.Close()
	})
}

// newMultiKeyTestChannel 插入一个 3 密钥、无 per-key 禁用状态的多密钥渠道。
func newMultiKeyTestChannel(t *testing.T, tag string) *model.Channel {
	t.Helper()
	channel := &model.Channel{
		Type:   1,
		Key:    "k0\nk1\nk2",
		Name:   "multi-key-fixture",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "gpt-4o-mini",
		Tag:    &tag,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 3,
		},
	}
	require.NoError(t, channel.Insert())
	return channel
}

// 预置的自动禁用诊断信息：断言"跳过已禁用索引"时必须逐字节比对，才能证明上游原因没被手动禁用覆盖。
const (
	upstreamDisableReason    = "upstream 401"
	upstreamDisableTimestamp = int64(1000)
)

// autoDisableKeysInDB 把 fixture 中指定索引的密钥预置成自动禁用（模拟线上流量把它们逐个 ban 掉），
// 同时保留 channel.Status = Enabled —— 这正是管理员从渠道列表把渠道重新启用之后的真实状态，
// 也是"删空密钥列表会连带把在线渠道打下线"这条回归的前置条件。
func autoDisableKeysInDB(t *testing.T, channel *model.Channel, indexes ...int) {
	t.Helper()
	channel.ChannelInfo.MultiKeyStatusList = make(map[int]int, len(indexes))
	channel.ChannelInfo.MultiKeyDisabledReason = make(map[int]string, len(indexes))
	channel.ChannelInfo.MultiKeyDisabledTime = make(map[int]int64, len(indexes))
	for _, idx := range indexes {
		channel.ChannelInfo.MultiKeyStatusList[idx] = common.ChannelStatusAutoDisabled
		channel.ChannelInfo.MultiKeyDisabledReason[idx] = upstreamDisableReason
		channel.ChannelInfo.MultiKeyDisabledTime[idx] = upstreamDisableTimestamp
	}
	require.NoError(t, channel.Update())
	require.Equal(t, common.ChannelStatusEnabled, channel.Status, "fixture 必须保持渠道启用")
}

// countEnabledAbilities 返回该渠道处于启用态的 ability 行数：Status 归一化是否连带
// 把渠道下线，只有这张表能证实（abilities.enabled 决定 relay 还能不能选到该渠道）。
func countEnabledAbilities(t *testing.T, channelID int) int64 {
	t.Helper()
	var enabled int64
	require.NoError(t, model.DB.Model(&model.Ability{}).
		Where("channel_id = ? AND enabled = ?", channelID, true).
		Count(&enabled).Error)
	return enabled
}

// callManageMultiKeys 直接调用 handler：审计写入需要 Log/User 两张表，
// role 取 root 让 authz.Can 短路放行（测试进程没有 casbin enforcer），
// 从而把断言集中在标签白名单这一维度——白名单只读上下文键，与角色无关。
func callManageMultiKeys(t *testing.T, managedTags []string, body map[string]interface{}) map[string]interface{} {
	t.Helper()
	payload, err := common.Marshal(body)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/channel/multi_key/manage", bytes.NewReader(payload))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("id", 1)
	c.Set("username", "root")
	c.Set("role", common.RoleRootUser)
	c.Set("admin_managed_tags", managedTags)

	ManageMultiKeys(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response map[string]interface{}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func TestManageMultiKeysAccessTokenRequiresSensitiveGrant(t *testing.T) {
	setupMultiKeyTestDB(t)
	channel := newMultiKeyTestChannel(t, "team-a")
	for _, tc := range []struct {
		name    string
		pat     bool
		legacy  bool
		scopes  []string
		success bool
	}{
		{name: "operate token cannot delete keys", pat: true, scopes: []string{service.AccessTokenScopeOf(authz.ChannelOperate)}},
		{name: "sensitive token can delete keys", pat: true, scopes: []string{service.AccessTokenScopeOf(authz.ChannelOperate), service.AccessTokenScopeOf(authz.ChannelSensitiveWrite)}, success: true},
		{name: "browser needs no token scopes", success: true},
		{name: "legacy retains user permissions", pat: true, legacy: true, success: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Restore the same three keys so every branch acts on the same target.
			channel.Key = "k0\nk1\nk2"
			channel.ChannelInfo.MultiKeySize = 3
			require.NoError(t, channel.Update())
			payload, err := common.Marshal(map[string]any{"channel_id": channel.Id, "action": "delete_key", "key_index": 1, "expected_key_count": 3})
			require.NoError(t, err)
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/multi_key/manage", bytes.NewReader(payload))
			ctx.Request.Header.Set("Content-Type", "application/json")
			ctx.Set("id", 1)
			ctx.Set("role", common.RoleRootUser)
			ctx.Set("use_access_token", tc.pat)
			ctx.Set("access_token_legacy", tc.legacy)
			ctx.Set("access_token_scopes", tc.scopes)
			ManageMultiKeys(ctx)
			var response map[string]any
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.Equal(t, tc.success, response["success"])
			stored, err := model.GetChannelById(channel.Id, true)
			require.NoError(t, err)
			if tc.success {
				assert.Equal(t, "k0\nk2", stored.Key)
			} else {
				assert.Equal(t, "k0\nk1\nk2", stored.Key)
			}
		})
	}
}

// TestResolveMultiKeyTargetIndexes 钉住批量索引校验：去重后按 len(keys) 判边界，
// 越界或负数整批拒绝且不返回部分结果——静默截断会让批量删除落到别的密钥上。
// 同时钉住拒绝携带的 error_code：前端据此拼 i18n 文案，中文 message 只是非 UI 客户端的兜底。
func TestResolveMultiKeyTargetIndexes(t *testing.T) {
	keys := []string{"k0", "k1", "k2"}

	cases := []struct {
		name        string
		indexes     []int
		wantSet     map[int]struct{}
		wantCode    string
		wantMessage string
	}{
		{
			name:        "a 空列表被拒绝",
			indexes:     nil,
			wantCode:    multiKeyErrCodeIndexesEmpty,
			wantMessage: "密钥索引列表为空",
		},
		{
			name:    "b 重复索引去重",
			indexes: []int{0, 2, 2},
			wantSet: map[int]struct{}{0: {}, 2: {}},
		},
		{
			name:        "c 负数索引被拒绝",
			indexes:     []int{-1},
			wantCode:    multiKeyErrCodeIndexOutOfRange,
			wantMessage: "密钥索引超出范围: -1",
		},
		{
			name:        "d 越界索引整批拒绝且不返回部分结果",
			indexes:     []int{0, 3},
			wantCode:    multiKeyErrCodeIndexOutOfRange,
			wantMessage: "密钥索引超出范围: 3",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			set, failure := resolveMultiKeyTargetIndexes(keys, c.indexes)
			if c.wantCode != "" {
				require.NotNil(t, failure)
				assert.Equal(t, c.wantCode, failure.code)
				assert.Equal(t, c.wantMessage, failure.message)
				assert.Nil(t, set)
				return
			}
			require.Nil(t, failure)
			assert.Equal(t, c.wantSet, set)
		})
	}
}

// TestManageMultiKeysRespectsManagedTags 钉住 ManageMultiKeys 的标签白名单校验。
// 正向用例（渠道标签在白名单内 → 未被拒绝）与反向用例同等重要：
// 它是防止这道 gate 被写成一律拒绝、把所有受标签限制的管理员全挡在门外的唯一保障。
func TestManageMultiKeysRespectsManagedTags(t *testing.T) {
	const managedTag = "team-a"

	actions := []struct {
		name string
		// newBody 每个子测试重新构造，避免共享 map 被 handler 或上一个用例改动
		newBody func(channelID int) map[string]interface{}
		// 放行时的期望终态
		wantKey        string
		wantStatusList map[int]int
	}{
		{
			name: "get_key_status",
			newBody: func(channelID int) map[string]interface{} {
				return map[string]interface{}{"channel_id": channelID, "action": "get_key_status"}
			},
			wantKey:        "k0\nk1\nk2",
			wantStatusList: nil,
		},
		{
			name: "disable_keys",
			newBody: func(channelID int) map[string]interface{} {
				return map[string]interface{}{
					"channel_id":  channelID,
					"action":      "disable_keys",
					"key_indexes": []int{0, 1},
					"reason":      "upstream 401",
				}
			},
			wantKey: "k0\nk1\nk2",
			wantStatusList: map[int]int{
				0: common.ChannelStatusManuallyDisabled,
				1: common.ChannelStatusManuallyDisabled,
			},
		},
		{
			name: "delete_keys",
			newBody: func(channelID int) map[string]interface{} {
				return map[string]interface{}{
					"channel_id":         channelID,
					"action":             "delete_keys",
					"key_indexes":        []int{0},
					"expected_key_count": 3,
				}
			},
			wantKey:        "k1\nk2",
			wantStatusList: map[int]int{},
		},
	}

	scenarios := []struct {
		name         string
		channelTag   string
		wantRejected bool
	}{
		{name: "标签不在白名单内则拒绝", channelTag: "team-b", wantRejected: true},
		{name: "标签在白名单内则放行", channelTag: managedTag, wantRejected: false},
	}

	for _, action := range actions {
		for _, scenario := range scenarios {
			t.Run(action.name+"/"+scenario.name, func(t *testing.T) {
				setupMultiKeyTestDB(t)
				channel := newMultiKeyTestChannel(t, scenario.channelTag)

				response := callManageMultiKeys(t, []string{managedTag}, action.newBody(channel.Id))

				reloaded, err := model.GetChannelById(channel.Id, true)
				require.NoError(t, err)

				if scenario.wantRejected {
					assert.Equal(t, false, response["success"])
					assert.Contains(t, response["message"], "无权管理该渠道")
					// 越权请求不得留下任何写入痕迹
					assert.Equal(t, "k0\nk1\nk2", reloaded.Key)
					assert.Empty(t, reloaded.ChannelInfo.MultiKeyStatusList)
					assert.Equal(t, 3, reloaded.ChannelInfo.MultiKeySize)
					return
				}

				assert.Equal(t, true, response["success"])
				assert.NotContains(t, response["message"], "无权管理该渠道")
				assert.Equal(t, action.wantKey, reloaded.Key)
				assert.Equal(t, action.wantStatusList, reloaded.ChannelInfo.MultiKeyStatusList)
			})
		}
	}
}

// TestManageMultiKeysDeleteDisabledKeysRefusesEmptyingKeyList 钉住 delete_disabled_keys 不再删空密钥列表。
// 删空这条路径是坏的：Update() 在 channel.Key == "" 时从 DB 读回旧列表，而 DB.Updates(channel)
// 又跳过零值 Key —— 密钥一个都删不掉，但三张 map 已清空、Status 被归一化成自动禁用、abilities 被关掉。
// 所以断言必须同时覆盖三件事：密钥列表原样保留、per-key 诊断原样保留、渠道仍然在线。
func TestManageMultiKeysDeleteDisabledKeysRefusesEmptyingKeyList(t *testing.T) {
	const managedTag = "team-a"

	cases := []struct {
		name string
		// autoDisabled 是预置为自动禁用（status 3）的索引
		autoDisabled   []int
		wantSuccess    bool
		wantErrorCode  string
		wantKey        string
		wantStatusList map[int]int
	}{
		{
			name:           "全部密钥都已自动禁用则整批拒绝",
			autoDisabled:   []int{0, 1, 2},
			wantSuccess:    false,
			wantErrorCode:  multiKeyErrCodeAllKeysDisabled,
			wantKey:        "k0\nk1\nk2",
			wantStatusList: map[int]int{0: 3, 1: 3, 2: 3},
		},
		{
			name:         "仍有可用密钥时照常删除并重编号",
			autoDisabled: []int{0, 2},
			wantSuccess:  true,
			wantKey:      "k1",
			// 存活的 k1 原本是启用态，RemoveMultiKeys 只为非启用的存活者写状态
			wantStatusList: map[int]int{},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setupMultiKeyTestDB(t)
			channel := newMultiKeyTestChannel(t, managedTag)
			autoDisableKeysInDB(t, channel, c.autoDisabled...)

			response := callManageMultiKeys(t, []string{managedTag}, map[string]interface{}{
				"channel_id": channel.Id,
				"action":     "delete_disabled_keys",
			})

			assert.Equal(t, c.wantSuccess, response["success"])
			if c.wantErrorCode != "" {
				assert.Equal(t, c.wantErrorCode, response["error_code"])
			} else {
				assert.NotContains(t, response, "error_code")
			}

			reloaded, err := model.GetChannelById(channel.Id, true)
			require.NoError(t, err)
			assert.Equal(t, c.wantKey, reloaded.Key)
			assert.Equal(t, c.wantStatusList, reloaded.ChannelInfo.MultiKeyStatusList)
			// 无论走哪条分支，渠道都必须保持可用：这是本回归的核心
			assert.Equal(t, common.ChannelStatusEnabled, reloaded.Status)
			assert.NotZero(t, countEnabledAbilities(t, channel.Id))

			if !c.wantSuccess {
				// 被拒绝时上游诊断必须逐字节保留
				assert.Equal(t, upstreamDisableReason, reloaded.ChannelInfo.MultiKeyDisabledReason[0])
				assert.Equal(t, upstreamDisableTimestamp, reloaded.ChannelInfo.MultiKeyDisabledTime[0])
				assert.Equal(t, 3, reloaded.ChannelInfo.MultiKeySize)
			}
		})
	}
}

// TestManageMultiKeysDisableKeyReportsNoOp 钉住 disable_key 不再对已禁用的密钥谎报成功。
// DisableKeys 有意跳过已处于禁用态的索引以保留上游诊断，所以 handler 必须看返回值：
// 陈旧的对话框标签页或 API 客户端对一个已被网关自动禁用的密钥发 disable_key 时，
// 响应不能声称一次并未发生的手动禁用。
// 同时钉住这条拒绝携带 error_code：单密钥 action 也必须给前端可拼 i18n 的机器可读码，
// 否则中文 message 会被原样 toast 给非中文管理员，而那一行还停在 Enabled。
func TestManageMultiKeysDisableKeyReportsNoOp(t *testing.T) {
	const managedTag = "team-a"

	cases := []struct {
		name        string
		keyIndex    int
		wantSuccess bool
		// wantErrorCode 为空表示成功响应里不应出现 error_code
		wantErrorCode string
		wantMessage   string
		wantStatus    int
		wantReason    string
		wantTime      int64
		// wantStatusList 是整张 per-key 状态表：逐键比对才能证明拒绝路径没有顺手改动别的密钥
		wantStatusList map[int]int
	}{
		{
			name:           "已自动禁用的密钥返回失败且保留上游诊断",
			keyIndex:       1,
			wantSuccess:    false,
			wantErrorCode:  multiKeyErrCodeKeyAlreadyDisabled,
			wantMessage:    "该密钥已处于禁用状态",
			wantStatus:     common.ChannelStatusAutoDisabled,
			wantReason:     upstreamDisableReason,
			wantTime:       upstreamDisableTimestamp,
			wantStatusList: map[int]int{1: common.ChannelStatusAutoDisabled},
		},
		{
			name:        "启用中的密钥正常禁用",
			keyIndex:    0,
			wantSuccess: true,
			wantMessage: "Key disabled",
			wantStatus:  common.ChannelStatusManuallyDisabled,
			wantReason:  "manual reason",
			wantStatusList: map[int]int{
				0: common.ChannelStatusManuallyDisabled,
				1: common.ChannelStatusAutoDisabled,
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setupMultiKeyTestDB(t)
			channel := newMultiKeyTestChannel(t, managedTag)
			autoDisableKeysInDB(t, channel, 1)

			response := callManageMultiKeys(t, []string{managedTag}, map[string]interface{}{
				"channel_id": channel.Id,
				"action":     "disable_key",
				"key_index":  c.keyIndex,
				"reason":     "manual reason",
			})

			assert.Equal(t, c.wantSuccess, response["success"])
			assert.Equal(t, c.wantMessage, response["message"])
			if c.wantErrorCode != "" {
				assert.Equal(t, c.wantErrorCode, response["error_code"])
			} else {
				assert.NotContains(t, response, "error_code")
			}
			// Single-key operations expose no counts; translated success replies use
			// data:null, while local failure replies omit it.
			if c.wantSuccess {
				assert.Equal(t, c.wantMessage, response["message_key"])
				assert.Contains(t, response, "data")
				assert.Nil(t, response["data"])
			} else {
				assert.NotContains(t, response, "data")
			}

			reloaded, err := model.GetChannelById(channel.Id, true)
			require.NoError(t, err)
			assert.Equal(t, c.wantStatus, reloaded.ChannelInfo.MultiKeyStatusList[c.keyIndex])
			assert.Equal(t, c.wantReason, reloaded.ChannelInfo.MultiKeyDisabledReason[c.keyIndex])
			if c.wantTime != 0 {
				assert.Equal(t, c.wantTime, reloaded.ChannelInfo.MultiKeyDisabledTime[c.keyIndex])
			}
			assert.Equal(t, "k0\nk1\nk2", reloaded.Key)
			assert.Equal(t, c.wantStatusList, reloaded.ChannelInfo.MultiKeyStatusList)

			if !c.wantSuccess {
				// 拒绝路径跳过 Update() 与缓存刷新，不得留下任何写入痕迹
				assert.Equal(t, 3, reloaded.ChannelInfo.MultiKeySize)
				assert.Equal(t, common.ChannelStatusEnabled, reloaded.Status)
				assert.NotZero(t, countEnabledAbilities(t, channel.Id))
			}
		})
	}
}

// TestManageMultiKeysBatchFailuresCarryErrorCodes 钉住三个批量 action 的业务失败都带稳定 error_code
// 与（可信时的）计数：前端把 response.message 直接 toast 出去，只靠中文串等于给非中文管理员看乱码文案。
// 同时断言拒绝路径不留任何写入痕迹。
func TestManageMultiKeysBatchFailuresCarryErrorCodes(t *testing.T) {
	const managedTag = "team-a"

	cases := []struct {
		name string
		// autoDisabled 在发请求之前预置为自动禁用的索引
		autoDisabled []int
		newBody      func(channelID int) map[string]interface{}
		wantCode     string
		wantMessage  string
		// wantCounts 为 nil 表示该失败没有可信计数，响应里不应出现 data
		wantCounts map[string]float64
	}{
		{
			name: "enable_keys 空索引列表",
			newBody: func(channelID int) map[string]interface{} {
				return map[string]interface{}{"channel_id": channelID, "action": "enable_keys"}
			},
			wantCode:    multiKeyErrCodeIndexesEmpty,
			wantMessage: "密钥索引列表为空",
		},
		{
			name: "disable_keys 索引越界",
			newBody: func(channelID int) map[string]interface{} {
				return map[string]interface{}{
					"channel_id":  channelID,
					"action":      "disable_keys",
					"key_indexes": []int{0, 7},
				}
			},
			wantCode:    multiKeyErrCodeIndexOutOfRange,
			wantMessage: "密钥索引超出范围: 7",
		},
		{
			name:         "disable_keys 选中的密钥全部已禁用",
			autoDisabled: []int{0, 1},
			newBody: func(channelID int) map[string]interface{} {
				return map[string]interface{}{
					"channel_id":  channelID,
					"action":      "disable_keys",
					"key_indexes": []int{0, 1},
				}
			},
			wantCode:    multiKeyErrCodeNoKeysToDisable,
			wantMessage: "没有可禁用的密钥",
			wantCounts:  map[string]float64{"affected_count": 0, "skipped_count": 2, "remaining_count": 3},
		},
		{
			name: "delete_keys 选中了全部密钥",
			newBody: func(channelID int) map[string]interface{} {
				return map[string]interface{}{
					"channel_id":  channelID,
					"action":      "delete_keys",
					"key_indexes": []int{0, 1, 2},
				}
			},
			wantCode:    multiKeyErrCodeCannotDeleteAll,
			wantMessage: "不能删除最后一个密钥",
			wantCounts:  map[string]float64{"affected_count": 0, "skipped_count": 3, "remaining_count": 3},
		},
		{
			name: "delete_keys 密钥列表已变更",
			newBody: func(channelID int) map[string]interface{} {
				return map[string]interface{}{
					"channel_id":         channelID,
					"action":             "delete_keys",
					"key_indexes":        []int{0},
					"expected_key_count": 5,
				}
			},
			wantCode:    multiKeyErrCodeKeyListChanged,
			wantMessage: "密钥列表已变更，请刷新后重试",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setupMultiKeyTestDB(t)
			channel := newMultiKeyTestChannel(t, managedTag)
			if len(c.autoDisabled) > 0 {
				autoDisableKeysInDB(t, channel, c.autoDisabled...)
			}

			response := callManageMultiKeys(t, []string{managedTag}, c.newBody(channel.Id))

			assert.Equal(t, false, response["success"])
			assert.Equal(t, c.wantCode, response["error_code"])
			assert.Equal(t, c.wantMessage, response["message"])

			if c.wantCounts == nil {
				assert.NotContains(t, response, "data")
			} else {
				counts, ok := response["data"].(map[string]interface{})
				require.True(t, ok, "该失败必须携带计数供前端拼文案")
				for field, want := range c.wantCounts {
					assert.Equal(t, want, counts[field], field)
				}
			}

			// 拒绝路径不得留下任何写入痕迹
			reloaded, err := model.GetChannelById(channel.Id, true)
			require.NoError(t, err)
			assert.Equal(t, "k0\nk1\nk2", reloaded.Key)
			assert.Equal(t, 3, reloaded.ChannelInfo.MultiKeySize)
			assert.Equal(t, common.ChannelStatusEnabled, reloaded.Status)
		})
	}
}

// TestManageMultiKeysBatchEnableDisableHonourExpectedKeyCount 钉住 enable_keys / disable_keys 的并发前置条件。
// 批量选择是针对某一份密钥列表快照算出来的，列表一变索引就重编号：操作员选中的 #4/#7/#10
// 会变成对如今坐在 3/6/9 的密钥的写入，还把他为另一批密钥审阅通过的 reason 原样盖上去。
// 正向用例（计数相符、字段缺省）与拒绝用例同等重要——它是防止这道 gate 被写成一律拒绝的唯一保障。
func TestManageMultiKeysBatchEnableDisableHonourExpectedKeyCount(t *testing.T) {
	const (
		managedTag   = "team-a"
		manualReason = "rotated by admin"
	)

	cases := []struct {
		name    string
		action  string
		indexes []int
		// sendExpected 为 false 表示请求完全不带 expected_key_count 字段（既有调用方的向后兼容路径），
		// 而不是带一个 0：带 0 会与 3 个密钥不符从而被拒绝，两者必须分得清
		sendExpected     bool
		expectedKeyCount int
		wantSuccess      bool
		wantErrorCode    string
		wantAffected     float64
		// wantStatusList / wantReason 是整张 per-key 表：逐键比对才能证明拒绝路径一字未改，
		// 且误写没有落到操作员没选的索引上
		wantStatusList map[int]int
		wantReason     map[int]string
	}{
		{
			name:             "a enable_keys 计数相符则照常启用",
			action:           "enable_keys",
			indexes:          []int{0},
			sendExpected:     true,
			expectedKeyCount: 3,
			wantSuccess:      true,
			wantAffected:     1,
			wantStatusList:   map[int]int{},
		},
		{
			name:             "b enable_keys 计数多一个则整批拒绝",
			action:           "enable_keys",
			indexes:          []int{0},
			sendExpected:     true,
			expectedKeyCount: 4,
			wantErrorCode:    multiKeyErrCodeKeyListChanged,
			wantStatusList:   map[int]int{0: common.ChannelStatusAutoDisabled},
			wantReason:       map[int]string{0: upstreamDisableReason},
		},
		{
			name:           "c enable_keys 不带该字段则照常启用",
			action:         "enable_keys",
			indexes:        []int{0},
			wantSuccess:    true,
			wantAffected:   1,
			wantStatusList: map[int]int{},
		},
		{
			name:             "d disable_keys 计数相符则照常禁用",
			action:           "disable_keys",
			indexes:          []int{1},
			sendExpected:     true,
			expectedKeyCount: 3,
			wantSuccess:      true,
			wantAffected:     1,
			wantStatusList: map[int]int{
				0: common.ChannelStatusAutoDisabled,
				1: common.ChannelStatusManuallyDisabled,
			},
			wantReason: map[int]string{0: upstreamDisableReason, 1: manualReason},
		},
		{
			name:             "e disable_keys 计数少一个则整批拒绝",
			action:           "disable_keys",
			indexes:          []int{1},
			sendExpected:     true,
			expectedKeyCount: 2,
			wantErrorCode:    multiKeyErrCodeKeyListChanged,
			wantStatusList:   map[int]int{0: common.ChannelStatusAutoDisabled},
			wantReason:       map[int]string{0: upstreamDisableReason},
		},
		{
			name:         "f disable_keys 不带该字段则照常禁用",
			action:       "disable_keys",
			indexes:      []int{1},
			wantSuccess:  true,
			wantAffected: 1,
			wantStatusList: map[int]int{
				0: common.ChannelStatusAutoDisabled,
				1: common.ChannelStatusManuallyDisabled,
			},
			wantReason: map[int]string{0: upstreamDisableReason, 1: manualReason},
		},
		{
			// 前置条件排在 resolveMultiKeyTargetIndexes 之前：陈旧列表上的越界索引要报成
			// key_list_changed 而非 key_index_out_of_range，两者对操作员意味着不同的事
			name:             "g enable_keys 陈旧列表优先于越界索引",
			action:           "enable_keys",
			indexes:          []int{5},
			sendExpected:     true,
			expectedKeyCount: 4,
			wantErrorCode:    multiKeyErrCodeKeyListChanged,
			wantStatusList:   map[int]int{0: common.ChannelStatusAutoDisabled},
			wantReason:       map[int]string{0: upstreamDisableReason},
		},
		{
			name:             "h disable_keys 陈旧列表优先于越界索引",
			action:           "disable_keys",
			indexes:          []int{5},
			sendExpected:     true,
			expectedKeyCount: 4,
			wantErrorCode:    multiKeyErrCodeKeyListChanged,
			wantStatusList:   map[int]int{0: common.ChannelStatusAutoDisabled},
			wantReason:       map[int]string{0: upstreamDisableReason},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setupMultiKeyTestDB(t)
			channel := newMultiKeyTestChannel(t, managedTag)
			// 预置索引 0 为自动禁用：三张 map 非空，拒绝路径才有东西可证明"一字未改"
			autoDisableKeysInDB(t, channel, 0)

			body := map[string]interface{}{
				"channel_id":  channel.Id,
				"action":      c.action,
				"key_indexes": c.indexes,
				"reason":      manualReason,
			}
			if c.sendExpected {
				body["expected_key_count"] = c.expectedKeyCount
			}

			response := callManageMultiKeys(t, []string{managedTag}, body)

			assert.Equal(t, c.wantSuccess, response["success"])
			if c.wantErrorCode != "" {
				assert.Equal(t, c.wantErrorCode, response["error_code"])
				assert.Equal(t, "密钥列表已变更，请刷新后重试", response["message"])
				// 列表已变更时没有可信计数可报，与 delete_keys 的同款拒绝一致地不写 data
				assert.NotContains(t, response, "data")
			} else {
				assert.NotContains(t, response, "error_code")
				counts, ok := response["data"].(map[string]interface{})
				require.True(t, ok, "成功响应必须携带计数")
				assert.Equal(t, c.wantAffected, counts["affected_count"])
				assert.Equal(t, float64(3), counts["remaining_count"])
			}

			reloaded, err := model.GetChannelById(channel.Id, true)
			require.NoError(t, err)
			// 两个 action 都不动密钥列表本身；拒绝时终态必须与预置状态逐字节相同
			assert.Equal(t, "k0\nk1\nk2", reloaded.Key)
			assert.Equal(t, 3, reloaded.ChannelInfo.MultiKeySize)
			assert.Equal(t, c.wantStatusList, reloaded.ChannelInfo.MultiKeyStatusList)
			assert.Equal(t, c.wantReason, reloaded.ChannelInfo.MultiKeyDisabledReason)
			// 时间戳表与 reason 表始终同键；手动禁用写入的时间取自 time.Now()，只能断言非零，
			// 预置的自动禁用则必须一字未改
			assert.Len(t, reloaded.ChannelInfo.MultiKeyDisabledTime, len(c.wantReason))
			for idx, reason := range c.wantReason {
				if reason == upstreamDisableReason {
					assert.Equal(t, upstreamDisableTimestamp, reloaded.ChannelInfo.MultiKeyDisabledTime[idx], idx)
				} else {
					assert.NotZero(t, reloaded.ChannelInfo.MultiKeyDisabledTime[idx], idx)
				}
			}
			// 索引 2 始终保持启用，所以 Status 归一化在任何分支都不该触发
			assert.Equal(t, common.ChannelStatusEnabled, reloaded.Status)
			assert.NotZero(t, countEnabledAbilities(t, channel.Id))
		})
	}
}

// TestManageMultiKeysSingleKeyActionsHonourExpectedKeyCount 钉住 disable_key / enable_key / delete_key
// 的并发前置条件——行内操作与批量面对的是同一个危险。管理对话框不随窗口聚焦刷新也没有轮询，
// 打开的标签页可以攥着一份数小时前的快照：另一个管理员删掉 #1 之后，其后的每个密钥都下移一位，
// 而第一个管理员的表格仍把索引 8 渲染成 #9。此时点删除会永久删掉他当作 #10 的那个密钥，
// 点禁用会把他为 #9 审阅通过的 reason 盖到别的密钥上，点启用则抹掉那个密钥的上游诊断并放它回轮询。
// 正向用例（计数相符、字段缺省）与拒绝用例同等重要——它是防止这道 gate 被写成一律拒绝的唯一保障。
func TestManageMultiKeysSingleKeyActionsHonourExpectedKeyCount(t *testing.T) {
	const (
		managedTag   = "team-a"
		manualReason = "rotated by admin"
	)

	// 拒绝路径的公共终态：与 autoDisableKeysInDB(0) 之后的预置状态逐字节相同
	var (
		rejectedStatusList = map[int]int{0: common.ChannelStatusAutoDisabled}
		rejectedReason     = map[int]string{0: upstreamDisableReason}
	)

	cases := []struct {
		name     string
		action   string
		keyIndex int
		// sendExpected 为 false 表示请求完全不带 expected_key_count 字段（既有调用方的向后兼容路径），
		// 而不是带一个 0：带 0 会与 3 个密钥不符从而被拒绝，两者必须分得清
		sendExpected     bool
		expectedKeyCount int
		wantSuccess      bool
		wantErrorCode    string
		wantMessage      string
		// 终态三件套：密钥列表本身、缓存派生的 MultiKeySize、整张 per-key 表。
		// 逐键比对才能证明拒绝路径一字未改，且误写没有落到操作员没选的索引上
		wantKey        string
		wantKeySize    int
		wantStatusList map[int]int
		wantReason     map[int]string
	}{
		{
			name:             "a enable_key 计数相符则照常启用",
			action:           "enable_key",
			keyIndex:         0,
			sendExpected:     true,
			expectedKeyCount: 3,
			wantSuccess:      true,
			wantMessage:      "Key enabled",
			wantKey:          "k0\nk1\nk2",
			wantKeySize:      3,
			wantStatusList:   map[int]int{},
		},
		{
			name:             "b enable_key 计数多一个则拒绝",
			action:           "enable_key",
			keyIndex:         0,
			sendExpected:     true,
			expectedKeyCount: 4,
			wantErrorCode:    multiKeyErrCodeKeyListChanged,
			wantKey:          "k0\nk1\nk2",
			wantKeySize:      3,
			wantStatusList:   rejectedStatusList,
			wantReason:       rejectedReason,
		},
		{
			name:           "c enable_key 不带该字段则照常启用",
			action:         "enable_key",
			keyIndex:       0,
			wantSuccess:    true,
			wantMessage:    "Key enabled",
			wantKey:        "k0\nk1\nk2",
			wantKeySize:    3,
			wantStatusList: map[int]int{},
		},
		{
			name:             "d disable_key 计数相符则照常禁用",
			action:           "disable_key",
			keyIndex:         1,
			sendExpected:     true,
			expectedKeyCount: 3,
			wantSuccess:      true,
			wantMessage:      "Key disabled",
			wantKey:          "k0\nk1\nk2",
			wantKeySize:      3,
			wantStatusList: map[int]int{
				0: common.ChannelStatusAutoDisabled,
				1: common.ChannelStatusManuallyDisabled,
			},
			wantReason: map[int]string{0: upstreamDisableReason, 1: manualReason},
		},
		{
			name:             "e disable_key 计数少一个则拒绝",
			action:           "disable_key",
			keyIndex:         1,
			sendExpected:     true,
			expectedKeyCount: 2,
			wantErrorCode:    multiKeyErrCodeKeyListChanged,
			wantKey:          "k0\nk1\nk2",
			wantKeySize:      3,
			wantStatusList:   rejectedStatusList,
			wantReason:       rejectedReason,
		},
		{
			name:        "f disable_key 不带该字段则照常禁用",
			action:      "disable_key",
			keyIndex:    1,
			wantSuccess: true,
			wantMessage: "Key disabled",
			wantKey:     "k0\nk1\nk2",
			wantKeySize: 3,
			wantStatusList: map[int]int{
				0: common.ChannelStatusAutoDisabled,
				1: common.ChannelStatusManuallyDisabled,
			},
			wantReason: map[int]string{0: upstreamDisableReason, 1: manualReason},
		},
		{
			name:             "g delete_key 计数相符则照常删除",
			action:           "delete_key",
			keyIndex:         1,
			sendExpected:     true,
			expectedKeyCount: 3,
			wantSuccess:      true,
			wantMessage:      "Key deleted",
			wantKey:          "k0\nk2",
			wantKeySize:      2,
			// 存活的 k0 仍是自动禁用且索引未变，k2 下移到 1 但处于启用态故不入表
			wantStatusList: map[int]int{0: common.ChannelStatusAutoDisabled},
			wantReason:     map[int]string{0: upstreamDisableReason},
		},
		{
			name:             "h delete_key 计数多一个则拒绝且密钥一个不少",
			action:           "delete_key",
			keyIndex:         1,
			sendExpected:     true,
			expectedKeyCount: 4,
			wantErrorCode:    multiKeyErrCodeKeyListChanged,
			wantKey:          "k0\nk1\nk2",
			wantKeySize:      3,
			wantStatusList:   rejectedStatusList,
			wantReason:       rejectedReason,
		},
		{
			name:           "i delete_key 不带该字段则照常删除",
			action:         "delete_key",
			keyIndex:       1,
			wantSuccess:    true,
			wantMessage:    "Key deleted",
			wantKey:        "k0\nk2",
			wantKeySize:    2,
			wantStatusList: map[int]int{0: common.ChannelStatusAutoDisabled},
			wantReason:     map[int]string{0: upstreamDisableReason},
		},
		{
			// 前置条件排在索引边界校验之前：陈旧列表上的越界索引要报成 key_list_changed
			// 而非"密钥索引超出范围"，两者对操作员意味着不同的事，前端各有文案
			name:             "j enable_key 陈旧列表优先于越界索引",
			action:           "enable_key",
			keyIndex:         5,
			sendExpected:     true,
			expectedKeyCount: 4,
			wantErrorCode:    multiKeyErrCodeKeyListChanged,
			wantKey:          "k0\nk1\nk2",
			wantKeySize:      3,
			wantStatusList:   rejectedStatusList,
			wantReason:       rejectedReason,
		},
		{
			name:             "k disable_key 陈旧列表优先于越界索引",
			action:           "disable_key",
			keyIndex:         5,
			sendExpected:     true,
			expectedKeyCount: 4,
			wantErrorCode:    multiKeyErrCodeKeyListChanged,
			wantKey:          "k0\nk1\nk2",
			wantKeySize:      3,
			wantStatusList:   rejectedStatusList,
			wantReason:       rejectedReason,
		},
		{
			name:             "l delete_key 陈旧列表优先于越界索引",
			action:           "delete_key",
			keyIndex:         5,
			sendExpected:     true,
			expectedKeyCount: 4,
			wantErrorCode:    multiKeyErrCodeKeyListChanged,
			wantKey:          "k0\nk1\nk2",
			wantKeySize:      3,
			wantStatusList:   rejectedStatusList,
			wantReason:       rejectedReason,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setupMultiKeyTestDB(t)
			channel := newMultiKeyTestChannel(t, managedTag)
			// 预置索引 0 为自动禁用：三张 map 非空，拒绝路径才有东西可证明"一字未改"
			autoDisableKeysInDB(t, channel, 0)

			body := map[string]interface{}{
				"channel_id": channel.Id,
				"action":     c.action,
				"key_index":  c.keyIndex,
				"reason":     manualReason,
			}
			if c.sendExpected {
				body["expected_key_count"] = c.expectedKeyCount
			}

			response := callManageMultiKeys(t, []string{managedTag}, body)

			assert.Equal(t, c.wantSuccess, response["success"])
			if c.wantErrorCode != "" {
				assert.Equal(t, c.wantErrorCode, response["error_code"])
				assert.Equal(t, "密钥列表已变更，请刷新后重试", response["message"])
			} else {
				assert.NotContains(t, response, "error_code")
				assert.Equal(t, c.wantMessage, response["message"])
			}
			// Successful single-key actions have a translated key and data:null;
			// rejection must retain its error code and omit untrusted counts.
			if c.wantSuccess {
				assert.Equal(t, c.wantMessage, response["message_key"])
				assert.Contains(t, response, "data")
				assert.Nil(t, response["data"])
			} else {
				assert.NotContains(t, response, "data")
			}

			reloaded, err := model.GetChannelById(channel.Id, true)
			require.NoError(t, err)
			assert.Equal(t, c.wantKey, reloaded.Key)
			assert.Equal(t, c.wantKeySize, reloaded.ChannelInfo.MultiKeySize)
			assert.Equal(t, c.wantStatusList, reloaded.ChannelInfo.MultiKeyStatusList)
			assert.Equal(t, c.wantReason, reloaded.ChannelInfo.MultiKeyDisabledReason)
			// 时间戳表与 reason 表始终同键；手动禁用写入的时间取自 time.Now()，只能断言非零，
			// 预置的自动禁用则必须一字未改
			assert.Len(t, reloaded.ChannelInfo.MultiKeyDisabledTime, len(c.wantReason))
			for idx, reason := range c.wantReason {
				if reason == upstreamDisableReason {
					assert.Equal(t, upstreamDisableTimestamp, reloaded.ChannelInfo.MultiKeyDisabledTime[idx], idx)
				} else {
					assert.NotZero(t, reloaded.ChannelInfo.MultiKeyDisabledTime[idx], idx)
				}
			}
			// 任何分支都至少留下一个启用密钥，Status 归一化不该把渠道打下线
			assert.Equal(t, common.ChannelStatusEnabled, reloaded.Status)
			assert.NotZero(t, countEnabledAbilities(t, channel.Id))
		})
	}
}

func TestMultiKeyEnableRestoresOnlyExhaustedChannels(t *testing.T) {
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	previousMaster, previousCache, previousRedis, previousSQLite := common.IsMasterNode, common.MemoryCacheEnabled, common.RedisEnabled, common.SQLitePath
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousType, previousLogType)
		common.IsMasterNode, common.MemoryCacheEnabled, common.RedisEnabled, common.SQLitePath = previousMaster, previousCache, previousRedis, previousSQLite
	})
	t.Setenv("SQL_DSN", os.Getenv("TEST_CHANNEL_SQL_DSN"))
	t.Setenv("LOG_SQL_DSN", "")
	common.IsMasterNode, common.MemoryCacheEnabled, common.RedisEnabled = false, false, false
	common.SQLitePath = filepath.Join(t.TempDir(), "channel.db")
	require.NoError(t, model.InitDB())
	database := model.DB
	sqlDB, err := database.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	model.LOG_DB = database
	common.SetLogDatabaseType(common.MainDatabaseType())
	require.NoError(t, database.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.User{}, &model.Log{}, &model.AuditLog{}))
	root := &model.User{Username: "multi-key-review-root", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	require.NoError(t, database.Create(root).Error)
	t.Cleanup(func() { require.NoError(t, database.Unscoped().Delete(root).Error) })
	versionQuery := "SELECT VERSION()"
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		versionQuery = "SELECT sqlite_version()"
	}
	var version string
	require.NoError(t, database.Raw(versionQuery).Scan(&version).Error)
	t.Logf("database=%s version=%s", common.MainDatabaseType(), version)

	for _, cacheEnabled := range []bool{false, true} {
		for _, action := range []string{"enable_key", "enable_all_keys"} {
			for _, tc := range []struct {
				name           string
				initialStatus  int
				manualOverride string
				wantStatus     int
			}{
				{name: "key exhaustion restores", initialStatus: common.ChannelStatusEnabled, wantStatus: common.ChannelStatusEnabled},
				{name: "manual disable is preserved", initialStatus: common.ChannelStatusManuallyDisabled, wantStatus: common.ChannelStatusManuallyDisabled},
				{name: "manual disable after exhaustion is preserved", initialStatus: common.ChannelStatusEnabled, manualOverride: "status", wantStatus: common.ChannelStatusManuallyDisabled},
				{name: "tag disable after exhaustion is preserved", initialStatus: common.ChannelStatusEnabled, manualOverride: "tag", wantStatus: common.ChannelStatusManuallyDisabled},
			} {
				t.Run(fmt.Sprintf("cache=%t/%s/%s", cacheEnabled, action, tc.name), func(t *testing.T) {
					common.MemoryCacheEnabled = cacheEnabled
					tag := t.Name()
					channel := &model.Channel{Name: t.Name(), Type: 1, Key: "key-one\nkey-two", Status: tc.initialStatus, Models: "test-model", Group: "default", Tag: &tag,
						ChannelInfo: model.ChannelInfo{IsMultiKey: true, MultiKeySize: 2, MultiKeyStatusList: map[int]int{1: common.ChannelStatusManuallyDisabled}},
					}
					require.NoError(t, channel.Insert())
					t.Cleanup(func() {
						require.NoError(t, channel.Delete())
						model.InitChannelCache()
					})
					for _, operation := range []string{"disable_key", action} {
						if operation == action {
							if tc.manualOverride == "status" {
								model.UpdateChannelStatus(channel.Id, "", common.ChannelStatusManuallyDisabled, "manual operation")
							} else if tc.manualOverride == "tag" {
								require.NoError(t, model.DisableChannelByTag(tag))
							}
						}
						payload, err := common.Marshal(MultiKeyManageRequest{ChannelId: channel.Id, Action: operation, KeyIndex: common.GetPointer(0)})
						require.NoError(t, err)
						recorder := httptest.NewRecorder()
						c, _ := gin.CreateTestContext(recorder)
						c.Set("id", root.Id)
						c.Set("role", common.RoleRootUser)
						c.Request = httptest.NewRequest(http.MethodPost, "/api/channel/multi_key", bytes.NewReader(payload))
						c.Request.Header.Set("Content-Type", "application/json")
						ManageMultiKeys(c)
						var result struct {
							Success bool `json:"success"`
						}
						require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &result))
						require.True(t, result.Success, recorder.Body.String())
					}
					loaded, err := model.GetChannelById(channel.Id, true)
					require.NoError(t, err)
					assert.Equal(t, tc.wantStatus, loaded.Status)
					assert.NotContains(t, loaded.ChannelInfo.MultiKeyStatusList, 0)
					assert.NotContains(t, loaded.ChannelInfo.MultiKeyDisabledReason, 0)
					assert.NotContains(t, loaded.ChannelInfo.MultiKeyDisabledTime, 0)
					var ability model.Ability
					require.NoError(t, database.Where("channel_id = ?", channel.Id).First(&ability).Error)
					assert.Equal(t, tc.wantStatus == common.ChannelStatusEnabled, ability.Enabled)
				})
			}
		}
	}
}

// TestMultiKeyBatchChangesCloseExhaustedChannels covers custom batch actions that bypass upstream's single-key handlers.
func TestMultiKeyBatchChangesCloseExhaustedChannels(t *testing.T) {
	for _, action := range []string{"disable_keys", "delete_keys"} {
		for _, exhausted := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/exhausted=%t", action, exhausted), func(t *testing.T) {
				setupMultiKeyTestDB(t)
				channel := newMultiKeyTestChannel(t, "team-a")
				disabled := []int{1}
				if exhausted {
					disabled = append(disabled, 2)
				}
				autoDisableKeysInDB(t, channel, disabled...)
				closed := make(map[string]string)
				for _, kind := range []string{wsmanager.KindRealtime, wsmanager.KindResponses} {
					t.Cleanup(wsmanager.Register(channel.Id, kind, func(reason string) {
						closed[kind] = reason
					}))
				}
				response := callManageMultiKeys(t, []string{"team-a"}, map[string]interface{}{
					"channel_id":         channel.Id,
					"action":             action,
					"key_indexes":        []int{0},
					"expected_key_count": 3,
				})
				require.Equal(t, true, response["success"], response)
				stored, err := model.GetChannelById(channel.Id, true)
				require.NoError(t, err)
				if exhausted {
					assert.Equal(t, common.ChannelStatusAutoDisabled, stored.Status)
					assert.Zero(t, countEnabledAbilities(t, channel.Id))
					assert.Equal(t, map[string]string{
						wsmanager.KindRealtime:  service.ChannelDisabledCloseReason,
						wsmanager.KindResponses: service.ChannelDisabledCloseReason,
					}, closed)
				} else {
					assert.Equal(t, common.ChannelStatusEnabled, stored.Status)
					assert.NotZero(t, countEnabledAbilities(t, channel.Id))
					assert.Empty(t, closed)
				}
			})
		}
	}
}
