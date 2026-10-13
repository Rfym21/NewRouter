package ratio_setting

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"new-router/setting/config"
	"new-router/types"
)

// defaultUserGroupRatio 为空:仅管理员显式配置的用户才有专属分组倍率。
var defaultUserGroupRatio = map[string]map[string]RatioEntry{}

// userGroupRatioMap 以字符串形式的用户 ID 为外层 key,使用分组为内层 key。
// 与 GroupGroupRatio(用户组×使用组)对称,但粒度细化到单个用户 ID。
// 内层值为 RatioEntry:override 固定覆盖,multiply 在该用户应生效倍率上打折。
var userGroupRatioMap = types.NewRWMap[string, map[string]RatioEntry]()

// UserGroupRatioSetting 为全局 option,仅经管理员 option 接口写入,
// 刻意不放入 dto.UserSetting——后者是用户自助可写,放入将导致用户自设 0 倍率白嫖。
type UserGroupRatioSetting struct {
	UserGroupRatio *types.RWMap[string, map[string]RatioEntry] `json:"user_group_ratio"`
}

var userGroupRatioSetting UserGroupRatioSetting

func init() {
	userGroupRatioMap.AddAll(defaultUserGroupRatio)
	userGroupRatioSetting = UserGroupRatioSetting{
		UserGroupRatio: userGroupRatioMap,
	}
	config.GlobalConfig.Register("user_group_ratio_setting", &userGroupRatioSetting)
}

// GetUserGroupRatioEntry 返回指定用户对某使用分组的专属倍率条目(含模式)。
// 命中返回 (entry, true);未配置返回 (RatioEntry{}, false),由调用方回退到组级倍率。
func GetUserGroupRatioEntry(userId int, usingGroup string) (RatioEntry, bool) {
	gp, ok := userGroupRatioMap.Get(strconv.Itoa(userId))
	if !ok {
		return RatioEntry{}, false
	}
	entry, ok := gp[usingGroup]
	if !ok {
		return RatioEntry{}, false
	}
	return entry, true
}

// GetUserGroupRatioCopy 返回用户专属倍率映射的快照。
func GetUserGroupRatioCopy() map[string]map[string]RatioEntry {
	return userGroupRatioMap.ReadAll()
}

// UserGroupRatio2JSONString 序列化当前用户专属倍率映射。
func UserGroupRatio2JSONString() string {
	return userGroupRatioMap.MarshalJSONString()
}

// UpdateUserGroupRatioByJSONString 用 JSON 覆盖用户专属倍率映射。
func UpdateUserGroupRatioByJSONString(jsonStr string) error {
	return types.LoadFromJsonString(userGroupRatioMap, jsonStr)
}

// CheckUserGroupRatio 校验用户专属倍率 JSON:模式合法且倍率不得为负。
func CheckUserGroupRatio(jsonStr string) error {
	checkUserGroupRatio := make(map[string]map[string]RatioEntry)
	if err := json.Unmarshal([]byte(jsonStr), &checkUserGroupRatio); err != nil {
		return err
	}
	for userId, groups := range checkUserGroupRatio {
		for group, entry := range groups {
			if err := validateRatioMode(entry.Mode); err != nil {
				return fmt.Errorf("%s/%s: %w", userId, group, err)
			}
			if entry.Value < 0 {
				return errors.New("user group ratio must be not less than 0: " + userId + "/" + group)
			}
		}
	}
	return nil
}
