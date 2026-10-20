package model

import (
	"context"
	"errors"
	"fmt"
	"time"

	"new-router/common"

	"gorm.io/gorm"
)

// ClearAuditLogsBefore only removes independent audit records. Usage logs,
// exports, and the cutoff timestamp itself are retained.
func ClearAuditLogsBefore(ctx context.Context, targetTimestamp int64) (int64, error) {
	if err := validateAuditCleanup(ctx, targetTimestamp); err != nil {
		return 0, err
	}
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		var total int64
		if err := LOG_DB.WithContext(ctx).Model(&AuditLog{}).Where("created_at < ?", targetTimestamp).Count(&total).Error; err != nil {
			return 0, err
		}
		if total == 0 {
			return 0, nil
		}
		if err := LOG_DB.WithContext(ctx).Exec("ALTER TABLE audit_logs DELETE WHERE created_at < ? SETTINGS mutations_sync = 1", targetTimestamp).Error; err != nil {
			return 0, err
		}
		return total, nil
	}
	result := LOG_DB.WithContext(ctx).Where("created_at < ?", targetTimestamp).Delete(&AuditLog{})
	return result.RowsAffected, result.Error
}

func validateAuditCleanup(ctx context.Context, targetTimestamp int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if targetTimestamp <= 0 || targetTimestamp > common.GetTimestamp() {
		return errors.New("invalid audit log cleanup timestamp")
	}
	if LOG_DB == nil {
		return errors.New("log database unavailable")
	}
	return nil
}

// ClearAuditLogsBeforeWithAudit records the mandatory completion event in the
// same database transaction as relational cleanup. ClickHouse mutations cannot
// transact with inserts, so their completion is recorded with a bounded context
// that survives client cancellation; a recording failure is returned explicitly.
func ClearAuditLogsBeforeWithAudit(ctx context.Context, targetTimestamp int64, entry AuditLog) (int64, error) {
	if err := validateAuditCleanup(ctx, targetTimestamp); err != nil {
		return 0, err
	}
	if entry.Username == "" {
		entry.Username, _ = GetUsernameById(entry.UserId, false)
	}
	if entry.RequestId == "" {
		entry.RequestId = common.NewRequestId()
	}
	entry.EventId = common.NewRequestId()
	entry.ActorRole = common.RoleRootUser
	entry.Category, entry.Action, entry.Success = AuditCategorySecurity, "audit_log.clear", true
	entry.Status = 200
	if entry.AuthMethod == "" {
		entry.AuthMethod = "session"
	}
	if ua := []rune(entry.UserAgent); len(ua) > 512 {
		entry.UserAgent = string(ua[:512])
	}
	writeCompletion := func(db *gorm.DB, deleted int64) error {
		entry.CreatedAt = max(common.GetTimestamp(), targetTimestamp)
		entry.Content = fmt.Sprintf("Cleared %d audit log entries before %d", deleted, targetTimestamp)
		entry.Other = AuditOther{Op: &AuditOperation{
			Action: entry.Action,
			Params: AuditFields{"target_timestamp": targetTimestamp, "deleted_count": deleted},
		}}
		var row any = &entry
		if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
			encoded, err := common.Marshal(entry.Other)
			if err != nil {
				return err
			}
			// Match RecordAuditLog's native-driver JSON binding.
			row = &struct {
				AuditLog     `gorm:"embedded"`
				EncodedOther string `gorm:"column:other;type:json"`
			}{AuditLog: entry, EncodedOther: string(encoded)}
		}
		return db.Table("audit_logs").Create(row).Error
	}
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		deleted, err := ClearAuditLogsBefore(ctx, targetTimestamp)
		if err != nil {
			return 0, err
		}
		auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := writeCompletion(LOG_DB.WithContext(auditCtx), deleted); err != nil {
			return deleted, fmt.Errorf("audit log cleanup completed but recording its completion failed: %w", err)
		}
		return deleted, nil
	}
	var deleted int64
	err := LOG_DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Where("created_at < ?", targetTimestamp).Delete(&AuditLog{})
		if result.Error != nil {
			return result.Error
		}
		deleted = result.RowsAffected
		return writeCompletion(tx, deleted)
	})
	if err != nil {
		return 0, err
	}
	return deleted, nil
}
