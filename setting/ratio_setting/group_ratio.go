package ratio_setting

import (
	"encoding/json"
	"errors"
	"fmt"

	"new-router/common"
	"new-router/setting/config"
	"new-router/types"
)

var defaultGroupRatio = map[string]float64{
	"default": 1,
	"vip":     1,
	"svip":    1,
}

var groupRatioMap = types.NewRWMap[string, float64]()

var defaultGroupGroupRatio = map[string]map[string]RatioEntry{
	"vip": {
		"edit_this": {Mode: RatioModeOverride, Value: 0.9},
	},
}

var groupGroupRatioMap = types.NewRWMap[string, map[string]RatioEntry]()

var defaultGroupSpecialUsableGroup = map[string]map[string]string{}

// defaultUserSpecialUsableGroup 为空:仅管理员显式配置的用户 ID 才有专属可用分组规则。
var defaultUserSpecialUsableGroup = map[string]map[string]string{}

type GroupRatioSetting struct {
	GroupRatio              *types.RWMap[string, float64]               `json:"group_ratio"`
	GroupGroupRatio         *types.RWMap[string, map[string]RatioEntry] `json:"group_group_ratio"`
	GroupSpecialUsableGroup *types.RWMap[string, map[string]string]     `json:"group_special_usable_group"`
	// UserSpecialUsableGroup 以字符串用户 ID 为外层 key,与 GroupSpecialUsableGroup 对称,
	// 但粒度细化到单个用户;规则叠加时在用户组规则之后应用,故用户级可覆盖组级。
	UserSpecialUsableGroup *types.RWMap[string, map[string]string] `json:"user_special_usable_group"`
}

var groupRatioSetting GroupRatioSetting

func init() {
	groupSpecialUsableGroup := types.NewRWMap[string, map[string]string]()
	groupSpecialUsableGroup.AddAll(defaultGroupSpecialUsableGroup)

	userSpecialUsableGroup := types.NewRWMap[string, map[string]string]()
	userSpecialUsableGroup.AddAll(defaultUserSpecialUsableGroup)

	groupRatioMap.AddAll(defaultGroupRatio)
	groupGroupRatioMap.AddAll(defaultGroupGroupRatio)

	groupRatioSetting = GroupRatioSetting{
		GroupSpecialUsableGroup: groupSpecialUsableGroup,
		UserSpecialUsableGroup:  userSpecialUsableGroup,
		GroupRatio:              groupRatioMap,
		GroupGroupRatio:         groupGroupRatioMap,
	}

	config.GlobalConfig.Register("group_ratio_setting", &groupRatioSetting)
}

func GetGroupRatioSetting() *GroupRatioSetting {
	if groupRatioSetting.GroupSpecialUsableGroup == nil {
		groupRatioSetting.GroupSpecialUsableGroup = types.NewRWMap[string, map[string]string]()
		groupRatioSetting.GroupSpecialUsableGroup.AddAll(defaultGroupSpecialUsableGroup)
	}
	if groupRatioSetting.UserSpecialUsableGroup == nil {
		groupRatioSetting.UserSpecialUsableGroup = types.NewRWMap[string, map[string]string]()
		groupRatioSetting.UserSpecialUsableGroup.AddAll(defaultUserSpecialUsableGroup)
	}
	return &groupRatioSetting
}

func GetGroupRatioCopy() map[string]float64 {
	return groupRatioMap.ReadAll()
}

func ContainsGroupRatio(name string) bool {
	_, ok := groupRatioMap.Get(name)
	return ok
}

func GroupRatio2JSONString() string {
	return groupRatioMap.MarshalJSONString()
}

func UpdateGroupRatioByJSONString(jsonStr string) error {
	return types.LoadFromJsonString(groupRatioMap, jsonStr)
}

func GetGroupRatio(name string) float64 {
	ratio, ok := groupRatioMap.Get(name)
	if !ok {
		common.SysLog(common.LogText("group ratio not found: %s", name))
		return 1
	}
	return ratio
}

// GetGroupGroupRatio 返回用户组对某使用分组的特殊倍率(已按模式解析为等效倍率)。
// multiply 模式返回 GetGroupRatio(usingGroup)*Value,故所有展示/计费调用方拿到的
// 都是可直接使用的绝对倍率。需要原始模式(如管理端回显)请用 GetGroupGroupRatioEntry。
func GetGroupGroupRatio(userGroup, usingGroup string) (float64, bool) {
	entry, ok := GetGroupGroupRatioEntry(userGroup, usingGroup)
	if !ok {
		return -1, false
	}
	return entry.Resolve(GetGroupRatio(usingGroup)), true
}

// GetGroupGroupRatioEntry 返回原始倍率条目(含模式),未命中返回 (RatioEntry{}, false)。
func GetGroupGroupRatioEntry(userGroup, usingGroup string) (RatioEntry, bool) {
	gp, ok := groupGroupRatioMap.Get(userGroup)
	if !ok {
		return RatioEntry{}, false
	}
	entry, ok := gp[usingGroup]
	if !ok {
		return RatioEntry{}, false
	}
	return entry, true
}

func GroupGroupRatio2JSONString() string {
	return groupGroupRatioMap.MarshalJSONString()
}

func UpdateGroupGroupRatioByJSONString(jsonStr string) error {
	return types.LoadFromJsonString(groupGroupRatioMap, jsonStr)
}

// CheckGroupGroupRatio 校验用户组特殊倍率 JSON:模式合法且倍率不得为负。
func CheckGroupGroupRatio(jsonStr string) error {
	checkGroupGroupRatio := make(map[string]map[string]RatioEntry)
	if err := json.Unmarshal([]byte(jsonStr), &checkGroupGroupRatio); err != nil {
		return err
	}
	for userGroup, groups := range checkGroupGroupRatio {
		for usingGroup, entry := range groups {
			if err := validateRatioMode(entry.Mode); err != nil {
				return fmt.Errorf("%s/%s: %w", userGroup, usingGroup, err)
			}
			if entry.Value < 0 {
				return errors.New("group group ratio must be not less than 0: " + userGroup + "/" + usingGroup)
			}
		}
	}
	return nil
}

func CheckGroupRatio(jsonStr string) error {
	checkGroupRatio := make(map[string]float64)
	err := json.Unmarshal([]byte(jsonStr), &checkGroupRatio)
	if err != nil {
		return err
	}
	for name, ratio := range checkGroupRatio {
		if ratio < 0 {
			return errors.New("group ratio must be not less than 0: " + name)
		}
	}
	return nil
}
