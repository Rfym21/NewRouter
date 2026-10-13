package model

import (
	"fmt"

	"new-router/common"

	"gorm.io/gorm"
)

// Keep expanded IN clauses below the lowest supported database parameter limit.
const batchUserIDChunkSize = 500

// BatchUserActionFilter describes the quota boundaries used by bulk user actions.
// Nil boundaries are not applied; non-nil zero values remain valid filters.
type BatchUserActionFilter struct {
	UsedQuotaMin      *int
	UsedQuotaMax      *int
	RemainingQuotaMin *int
	RemainingQuotaMax *int
	TotalQuotaMin     *int
	TotalQuotaMax     *int
}

// HasConditions reports whether at least one quota boundary is present.
func (filter BatchUserActionFilter) HasConditions() bool {
	return filter.UsedQuotaMin != nil || filter.UsedQuotaMax != nil ||
		filter.RemainingQuotaMin != nil || filter.RemainingQuotaMax != nil ||
		filter.TotalQuotaMin != nil || filter.TotalQuotaMax != nil
}

func usersByQuotaFilter(tx *gorm.DB, filter BatchUserActionFilter) *gorm.DB {
	query := tx.Model(&User{}).Where("role < ?", common.RoleAdminUser)
	if filter.UsedQuotaMin != nil {
		query = query.Where("used_quota >= ?", *filter.UsedQuotaMin)
	}
	if filter.UsedQuotaMax != nil {
		query = query.Where("used_quota <= ?", *filter.UsedQuotaMax)
	}
	if filter.RemainingQuotaMin != nil {
		query = query.Where("quota >= ?", *filter.RemainingQuotaMin)
	}
	if filter.RemainingQuotaMax != nil {
		query = query.Where("quota <= ?", *filter.RemainingQuotaMax)
	}
	if filter.TotalQuotaMin != nil {
		query = query.Where(totalUserQuotaSQLExpression()+" >= ?", *filter.TotalQuotaMin)
	}
	if filter.TotalQuotaMax != nil {
		query = query.Where(totalUserQuotaSQLExpression()+" <= ?", *filter.TotalQuotaMax)
	}
	return query
}

func totalUserQuotaSQLExpression() string {
	if common.UsingMainDatabase(common.DatabaseTypeMySQL) {
		return "CAST(quota AS SIGNED) + CAST(used_quota AS SIGNED)"
	}
	return "CAST(quota AS BIGINT) + CAST(used_quota AS BIGINT)"
}

// CountUsersByQuotaFilter returns the number of active ordinary users matching filter.
func CountUsersByQuotaFilter(filter BatchUserActionFilter) (int64, error) {
	var count int64
	if err := usersByQuotaFilter(DB, filter).Count(&count).Error; err != nil {
		return 0, fmt.Errorf("count users by quota filter: %w", err)
	}
	return count, nil
}

func invalidateBatchUserCaches(userIDs []int, authVersions map[int]int64) {
	for _, userID := range userIDs {
		if err := publishCommittedUserAuthVersion(userID, authVersions[userID]); err != nil {
			common.SysLog(fmt.Sprintf("failed to publish batch user auth version: user_id=%d, error=%v", userID, err))
		}
		if _, err := RevokeAllUserSessions(userID, "admin_batch_action"); err != nil {
			common.SysLog(fmt.Sprintf("failed to revoke batch user sessions: user_id=%d, error=%v", userID, err))
		}
		if err := invalidateUserCache(userID); err != nil {
			common.SysLog(fmt.Sprintf("failed to invalidate user cache after batch action: user_id=%d, error=%v", userID, err))
		}
		if err := InvalidateUserTokensCache(userID); err != nil {
			common.SysLog(fmt.Sprintf("failed to invalidate token cache after batch action: user_id=%d, error=%v", userID, err))
		}
	}
}

// BatchDisableUsersByQuotaFilter disables matching ordinary users and returns the affected row count.
func BatchDisableUsersByQuotaFilter(filter BatchUserActionFilter) (int64, error) {
	var userIDs []int
	authVersions := make(map[int]int64)
	var affected int64
	err := DB.Transaction(func(tx *gorm.DB) error {
		query := usersByQuotaFilter(tx, filter).Where("status = ?", common.UserStatusEnabled)
		if err := lockForUpdate(query).Pluck("id", &userIDs).Error; err != nil {
			return fmt.Errorf("select users to disable: %w", err)
		}
		if len(userIDs) == 0 {
			return nil
		}
		for _, userID := range userIDs {
			next, err := IncrementUserAuthVersionWithTx(tx, userID)
			if err != nil {
				return fmt.Errorf("advance batch user authentication version: %w", err)
			}
			authVersions[userID] = next
		}

		for start := 0; start < len(userIDs); start += batchUserIDChunkSize {
			end := min(start+batchUserIDChunkSize, len(userIDs))
			result := tx.Model(&User{}).
				Where("id IN ?", userIDs[start:end]).
				Where("role < ?", common.RoleAdminUser).
				Where("status = ?", common.UserStatusEnabled).
				Update("status", common.UserStatusDisabled)
			if result.Error != nil {
				return fmt.Errorf("disable users by quota filter: %w", result.Error)
			}
			affected += result.RowsAffected
		}
		return nil
	})
	if err != nil {
		return 0, err
	}

	invalidateBatchUserCaches(userIDs, authVersions)
	return affected, nil
}

// BatchSoftDeleteUsersByQuotaFilter soft-deletes matching ordinary users and returns the affected row count.
func BatchSoftDeleteUsersByQuotaFilter(filter BatchUserActionFilter) (int64, error) {
	var userIDs []int
	authVersions := make(map[int]int64)
	var affected int64
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(usersByQuotaFilter(tx, filter)).Pluck("id", &userIDs).Error; err != nil {
			return fmt.Errorf("select users to delete: %w", err)
		}
		if len(userIDs) == 0 {
			return nil
		}
		for _, userID := range userIDs {
			next, err := IncrementUserAuthVersionWithTx(tx, userID)
			if err != nil {
				return fmt.Errorf("advance batch user authentication version: %w", err)
			}
			authVersions[userID] = next
		}

		for start := 0; start < len(userIDs); start += batchUserIDChunkSize {
			end := min(start+batchUserIDChunkSize, len(userIDs))
			result := tx.
				Where("id IN ?", userIDs[start:end]).
				Where("role < ?", common.RoleAdminUser).
				Delete(&User{})
			if result.Error != nil {
				return fmt.Errorf("soft-delete users by quota filter: %w", result.Error)
			}
			affected += result.RowsAffected
		}
		return nil
	})
	if err != nil {
		return 0, err
	}

	invalidateBatchUserCaches(userIDs, authVersions)
	return affected, nil
}

// PurgeSoftDeletedUsers permanently removes soft-deleted non-root users and their custom OAuth bindings.
// Root records are retained defensively even though normal account flows cannot soft-delete them.
func PurgeSoftDeletedUsers() (int64, error) {
	var userIDs []int
	var purgedTokens []Token
	authVersions := make(map[int]int64)
	var affected int64
	err := DB.Transaction(func(tx *gorm.DB) error {
		query := tx.Unscoped().Model(&User{}).
			Where("deleted_at IS NOT NULL").
			Where("role < ?", common.RoleRootUser)
		if err := lockForUpdate(query).Pluck("id", &userIDs).Error; err != nil {
			return fmt.Errorf("select soft-deleted users to purge: %w", err)
		}
		if len(userIDs) == 0 {
			return nil
		}
		for _, userID := range userIDs {
			next, err := IncrementUserAuthVersionWithTx(tx, userID)
			if err != nil {
				return fmt.Errorf("advance batch user authentication version: %w", err)
			}
			authVersions[userID] = next
			if common.RedisEnabled {
				var tokens []Token
				if err := tx.Unscoped().Select("id", commonKeyCol).Where("user_id = ?", userID).Find(&tokens).Error; err != nil {
					return err
				}
				purgedTokens = append(purgedTokens, tokens...)
			}
			if err := deleteUserAuthenticationData(tx, userID); err != nil {
				return fmt.Errorf("delete authentication data for purged user: %w", err)
			}
		}

		for start := 0; start < len(userIDs); start += batchUserIDChunkSize {
			end := min(start+batchUserIDChunkSize, len(userIDs))
			userIDChunk := userIDs[start:end]
			result := tx.Unscoped().
				Where("id IN ?", userIDChunk).
				Where("role < ?", common.RoleRootUser).
				Delete(&User{})
			if result.Error != nil {
				return fmt.Errorf("purge soft-deleted users: %w", result.Error)
			}
			affected += result.RowsAffected
		}
		return nil
	})
	if err != nil {
		return 0, err
	}

	if err := invalidateTokensCache(purgedTokens); err != nil {
		common.SysLog("failed to invalidate tokens after purging users: " + err.Error())
	}
	invalidateBatchUserCaches(userIDs, authVersions)
	return affected, nil
}
