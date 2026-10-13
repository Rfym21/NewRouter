package setting

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"

	"new-router/common"
)

// maxRateLimitDurationSeconds is the largest window the count cap is computed
// against (24h). Token-bucket capacity is count*duration; this keeps that
// product inside int64 when the window is at most a day.
const maxRateLimitDurationSeconds = 24 * 60 * 60

// maxModelRequestRateLimitCount is math.MaxInt64 / maxRateLimitDurationSeconds.
// It is the largest count that cannot overflow int64(count)*duration for a
// window of at most 24 hours.
const maxModelRequestRateLimitCount int64 = math.MaxInt64 / maxRateLimitDurationSeconds

const modelRequestRateLimitUserDefaultGroup = "*"

// ModelRequestRateLimitEnabled controls whether model request rate limiting is active.
var ModelRequestRateLimitEnabled = false

// ModelRequestRateLimitDurationMinutes is the shared limit window in minutes.
var ModelRequestRateLimitDurationMinutes = 1

// ModelRequestRateLimitCount is the global total request limit per user.
var ModelRequestRateLimitCount = 0

// ModelRequestRateLimitSuccessCount is the global successful request limit per user.
var ModelRequestRateLimitSuccessCount = 1000

// ModelRequestRateLimitGroup stores group-specific request limits.
var ModelRequestRateLimitGroup = map[string][2]int{}

// ModelRequestRateLimitUser stores user-specific request limits by user ID and group.
var ModelRequestRateLimitUser = map[string]map[string][2]int{}

// ModelRequestRateLimitMutex protects model request rate limit settings.
var ModelRequestRateLimitMutex sync.RWMutex

// ModelRequestRateLimitGroup2JSONString serializes group-specific model request limits.
func ModelRequestRateLimitGroup2JSONString() string {
	ModelRequestRateLimitMutex.RLock()
	defer ModelRequestRateLimitMutex.RUnlock()

	jsonBytes, err := common.Marshal(ModelRequestRateLimitGroup)
	if err != nil {
		common.SysLog(common.LogText("error marshalling model ratio: %s", err.Error()))
	}
	return string(jsonBytes)
}

// ModelRequestRateLimitUser2JSONString serializes user-specific model request limits.
func ModelRequestRateLimitUser2JSONString() string {
	ModelRequestRateLimitMutex.RLock()
	defer ModelRequestRateLimitMutex.RUnlock()

	jsonBytes, err := common.Marshal(ModelRequestRateLimitUser)
	if err != nil {
		common.SysLog("error marshalling model request rate limit user: " + err.Error())
	}
	return string(jsonBytes)
}

// UpdateModelRequestRateLimitGroupByJSONString replaces group-specific model request limits.
func UpdateModelRequestRateLimitGroupByJSONString(jsonStr string) error {
	next, err := parseModelRequestRateLimitGroup(jsonStr)
	if err != nil {
		return err
	}

	ModelRequestRateLimitMutex.Lock()
	defer ModelRequestRateLimitMutex.Unlock()

	ModelRequestRateLimitGroup = next
	return nil
}

// UpdateModelRequestRateLimitUserByJSONString replaces user-specific model request limits.
func UpdateModelRequestRateLimitUserByJSONString(jsonStr string) error {
	next, err := parseModelRequestRateLimitUser(jsonStr)
	if err != nil {
		return err
	}

	ModelRequestRateLimitMutex.Lock()
	defer ModelRequestRateLimitMutex.Unlock()

	ModelRequestRateLimitUser = next
	return nil
}

// GetGroupRateLimit returns a group-specific model request limit.
func GetGroupRateLimit(group string) (totalCount, successCount int, found bool) {
	ModelRequestRateLimitMutex.RLock()
	defer ModelRequestRateLimitMutex.RUnlock()

	if ModelRequestRateLimitGroup == nil {
		return 0, 0, false
	}

	limits, found := ModelRequestRateLimitGroup[group]
	if !found {
		return 0, 0, false
	}
	return limits[0], limits[1], true
}

// GetUserRateLimit returns the user+group limit, falling back to the user's default rule.
func GetUserRateLimit(userID int, group string) (totalCount, successCount int, found bool) {
	if userID <= 0 {
		return 0, 0, false
	}

	ModelRequestRateLimitMutex.RLock()
	defer ModelRequestRateLimitMutex.RUnlock()

	userLimits, found := ModelRequestRateLimitUser[strconv.Itoa(userID)]
	if !found {
		return 0, 0, false
	}

	if group != "" {
		limits, ok := userLimits[group]
		if ok {
			return limits[0], limits[1], true
		}
	}

	limits, found := userLimits[modelRequestRateLimitUserDefaultGroup]
	if !found {
		return 0, 0, false
	}
	return limits[0], limits[1], true
}

// CheckModelRequestRateLimitGroup validates group-specific model request limits.
func CheckModelRequestRateLimitGroup(jsonStr string) error {
	_, err := parseModelRequestRateLimitGroup(jsonStr)
	return err
}

// CheckModelRequestRateLimitUser validates user-specific model request limits.
func CheckModelRequestRateLimitUser(jsonStr string) error {
	_, err := parseModelRequestRateLimitUser(jsonStr)
	return err
}

func parseModelRequestRateLimitGroup(jsonStr string) (map[string][2]int, error) {
	next := make(map[string][2]int)
	if strings.TrimSpace(jsonStr) == "" {
		return next, nil
	}
	raw := make(map[string][]int)
	if err := common.UnmarshalJsonStr(jsonStr, &raw); err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, fmt.Errorf("group rate limits must be a JSON object")
	}
	for group, limits := range raw {
		if strings.TrimSpace(group) == "" {
			return nil, fmt.Errorf("group name cannot be empty")
		}
		if len(limits) != 2 {
			return nil, fmt.Errorf("group %s must contain exactly two rate limit values", group)
		}
		values := [2]int{limits[0], limits[1]}
		if err := validateModelRequestRateLimitValues("group "+group, values); err != nil {
			return nil, err
		}
		next[group] = values
	}
	return next, nil
}

func parseModelRequestRateLimitUser(jsonStr string) (map[string]map[string][2]int, error) {
	next := make(map[string]map[string][2]int)
	if strings.TrimSpace(jsonStr) == "" {
		return next, nil
	}
	raw := make(map[string]map[string][]int)
	if err := common.UnmarshalJsonStr(jsonStr, &raw); err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, fmt.Errorf("user rate limits must be a JSON object")
	}
	for userID, groupLimits := range raw {
		id, err := strconv.Atoi(userID)
		if err != nil || id <= 0 || strconv.Itoa(id) != userID {
			return nil, fmt.Errorf("user %s is not a valid positive user id", userID)
		}
		if len(groupLimits) == 0 {
			return nil, fmt.Errorf("user %s has no rate limit rules", userID)
		}
		for group, limits := range groupLimits {
			if strings.TrimSpace(group) == "" {
				return nil, fmt.Errorf("user %s has empty group name", userID)
			}
			if len(limits) != 2 {
				return nil, fmt.Errorf("user %s group %s must contain exactly two rate limit values", userID, group)
			}
			values := [2]int{limits[0], limits[1]}
			if err := validateModelRequestRateLimitValues("user "+userID+" group "+group, values); err != nil {
				return nil, err
			}
			if next[userID] == nil {
				next[userID] = make(map[string][2]int, len(groupLimits))
			}
			next[userID][group] = values
		}
	}
	return next, nil
}

func validateModelRequestRateLimitValues(scope string, limits [2]int) error {
	if limits[0] < 0 || limits[1] < 1 {
		return fmt.Errorf("%s has invalid rate limit values: [%d, %d]", scope, limits[0], limits[1])
	}
	if int64(limits[0]) > maxModelRequestRateLimitCount || int64(limits[1]) > maxModelRequestRateLimitCount {
		return fmt.Errorf("%s [%d, %d] exceeds max rate limit %d", scope, limits[0], limits[1], maxModelRequestRateLimitCount)
	}
	return nil
}
