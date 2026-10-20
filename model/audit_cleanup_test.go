package model

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"new-router/common"
)

func TestAuditCleanupSeparateLogDatabase(t *testing.T) {
	for _, scenario := range []string{"success", "audit insert fails", "canceled before delete", "canceled after delete"} {
		t.Run(scenario, func(t *testing.T) {
			mainDB, err := gorm.Open(sqlite.Open(t.TempDir()+"/main.db"), &gorm.Config{})
			require.NoError(t, err)
			logDB, err := gorm.Open(sqlite.Open(t.TempDir()+"/audit.db"), &gorm.Config{})
			require.NoError(t, err)
			for _, db := range []*gorm.DB{mainDB, logDB} {
				sqlDB, err := db.DB()
				require.NoError(t, err)
				sqlDB.SetMaxOpenConns(1)
				t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })
				require.NoError(t, db.AutoMigrate(&AuditLog{}, &Log{}))
			}
			previousDB, previousLogDB, previousType := DB, LOG_DB, common.LogDatabaseType()
			DB, LOG_DB = mainDB, logDB
			common.SetLogDatabaseType(common.DatabaseTypeSQLite)
			t.Cleanup(func() {
				DB, LOG_DB = previousDB, previousLogDB
				common.SetLogDatabaseType(previousType)
			})
			cutoff := time.Now().Add(-time.Hour).Unix()
			require.NoError(t, mainDB.Create(&AuditLog{EventId: "main-db-retained", CreatedAt: cutoff - 2}).Error)
			require.NoError(t, logDB.Create(&[]AuditLog{{EventId: "older", CreatedAt: cutoff - 1}, {EventId: "at-cutoff", CreatedAt: cutoff}}).Error)
			require.NoError(t, logDB.Create(&Log{CreatedAt: cutoff - 1, Content: "usage retained"}).Error)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch scenario {
			case "audit insert fails":
				require.NoError(t, logDB.Callback().Create().Before("gorm:create").Register("audit_cleanup_insert_failure", func(tx *gorm.DB) {
					if tx.Statement.Table == "audit_logs" {
						tx.AddError(errors.New("audit insert failed"))
					}
				}))
			case "canceled before delete":
				cancel()
			case "canceled after delete":
				require.NoError(t, logDB.Callback().Delete().After("gorm:delete").Register("audit_cleanup_cancel", func(tx *gorm.DB) { cancel() }))
			}
			deleted, err := ClearAuditLogsBeforeWithAudit(ctx, cutoff, AuditLog{UserId: 1, Username: "root"})
			if scenario == "success" {
				require.NoError(t, err)
				assert.EqualValues(t, 1, deleted)
				var entry AuditLog
				require.NoError(t, logDB.Where("action = ?", "audit_log.clear").Take(&entry).Error)
				assert.GreaterOrEqual(t, entry.CreatedAt, cutoff)
				assert.Equal(t, AuditCategorySecurity, entry.Category)
				require.NotNil(t, entry.Other.Op)
				metadata, err := common.Marshal(entry.Other.Op.Params)
				require.NoError(t, err)
				assert.JSONEq(t, fmt.Sprintf(`{"target_timestamp":%d,"deleted_count":1}`, cutoff), string(metadata))
			} else {
				require.Error(t, err)
				assert.Zero(t, deleted)
			}
			var older, usage, mainCount int64
			require.NoError(t, logDB.Model(&AuditLog{}).Where("created_at < ?", cutoff).Count(&older).Error)
			require.NoError(t, logDB.Model(&Log{}).Count(&usage).Error)
			require.NoError(t, mainDB.Model(&AuditLog{}).Count(&mainCount).Error)
			assert.EqualValues(t, 1, usage)
			assert.EqualValues(t, 1, mainCount)
			if scenario == "success" {
				assert.Zero(t, older)
			} else {
				assert.EqualValues(t, 1, older, "a failed completion audit or canceled request rolls back deletion")
			}
		})
	}
}
