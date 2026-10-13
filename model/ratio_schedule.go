package model

import (
	"errors"

	"new-router/common"
)

// 定时倍率窗口的模式常量,与 setting/ratio_setting 的 RatioMode* 保持一致。
const (
	RatioScheduleModeOverride = "override"
	RatioScheduleModeMultiply = "multiply"
)

// RatioSchedule 是一条每日限时倍率窗口。窗口以本地时区的「一天内分钟数」
// [StartMinute,EndMinute) 表示,支持跨午夜(StartMinute>EndMinute)。窗口内在价格
// 计算时叠加/覆盖 GroupName 的倍率,窗口外自动恢复,不再永久改写 GroupRatio option。
//
// 历史遗留列 FireAt/Recurring 保留仅为兼容 AutoMigrate(不 drop),新逻辑不再读取。
type RatioSchedule struct {
	Id                 int     `json:"id" gorm:"primaryKey"`
	GroupName          string  `json:"group_name" gorm:"type:varchar(64);index"`
	StartMinute        int     `json:"start_minute" gorm:"default:0"` // 窗口起点:本地时区一天内分钟数 0..1439
	EndMinute          int     `json:"end_minute" gorm:"default:0"`   // 窗口终点:同上,start>end 表示跨午夜
	Mode               string  `json:"mode" gorm:"type:varchar(16);default:'multiply'"`
	TargetRatio        float64 `json:"target_ratio"`                               // override 为目标倍率,multiply 为折扣系数
	StackWithUserRatio bool    `json:"stack_with_user_ratio" gorm:"default:false"` // multiply 时是否叠加在专属倍率上
	Enabled            bool    `json:"enabled" gorm:"default:true"`
	FireAt             int64   `json:"-" gorm:"bigint;index"`                // 遗留列,不再使用
	Recurring          string  `json:"-" gorm:"type:varchar(16);default:''"` // 遗留列,不再使用
	CreatedAt          int64   `json:"created_at" gorm:"bigint"`
}

// GetAllRatioSchedules 返回全部窗口,按 Id 升序(供缓存与列表展示的确定性顺序)。
func GetAllRatioSchedules() ([]*RatioSchedule, error) {
	var schedules []*RatioSchedule
	err := DB.Order("id asc").Find(&schedules).Error
	return schedules, err
}

// CreateRatioSchedule 校验并写入一条窗口。分钟范围、模式与目标值的校验由 controller 层承担,
// 这里做最后一道防线。
func CreateRatioSchedule(s *RatioSchedule) error {
	if s.GroupName == "" {
		return errors.New("group name is empty")
	}
	if s.StartMinute < 0 || s.StartMinute > 1439 || s.EndMinute < 0 || s.EndMinute > 1439 {
		return errors.New("minute out of range [0,1439]")
	}
	if s.StartMinute == s.EndMinute {
		return errors.New("start and end minute must differ")
	}
	if s.Mode != RatioScheduleModeOverride && s.Mode != RatioScheduleModeMultiply {
		return errors.New("unsupported mode: " + s.Mode)
	}
	if s.TargetRatio < 0 {
		return errors.New("target ratio must be not less than 0")
	}
	s.CreatedAt = common.GetTimestamp()
	return DB.Create(s).Error
}

// DeleteRatioSchedule 删除指定窗口。
func DeleteRatioSchedule(id int) error {
	return DB.Delete(&RatioSchedule{}, id).Error
}
