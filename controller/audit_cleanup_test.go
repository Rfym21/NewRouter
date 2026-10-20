package controller

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"new-router/common"
	"new-router/model"
	"new-router/service"
	"new-router/setting/operation_setting"
	"new-router/setting/system_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAuditCleanupRequiresBoundSingleUseProof(t *testing.T) {
	for _, scenario := range []string{"missing", "wrong scope", "wrong cutoff", "expired", "consumed", "other session", "admin", "access token", "success"} {
		t.Run(scenario, func(t *testing.T) {
			user, identity := setupSecurityEnrollmentTest(t)
			previousMode := operation_setting.AdminAuditMode
			operation_setting.AdminAuditMode = operation_setting.AdminAuditModeBillingOnly
			t.Cleanup(func() { operation_setting.AdminAuditMode = previousMode })
			user.Role = common.RoleRootUser
			require.NoError(t, model.DB.Model(user).Update("role", user.Role).Error)
			require.NoError(t, model.PublishUserAuthCache(user.Id))
			require.NoError(t, model.LOG_DB.AutoMigrate(&model.Log{}))
			cutoff := time.Now().Add(-time.Hour).Unix()
			records := []model.AuditLog{
				{EventId: "older-a", CreatedAt: cutoff - 1, ActorRole: common.RoleRootUser, Category: model.AuditCategorySecurity},
				{EventId: "older-b", CreatedAt: cutoff - 2, ActorRole: common.RoleCommonUser, Category: model.AuditCategoryLogin},
				{EventId: "at-cutoff", CreatedAt: cutoff, ActorRole: common.RoleRootUser},
				{EventId: "newer", CreatedAt: cutoff + 1, ActorRole: common.RoleAdminUser},
			}
			require.NoError(t, model.LOG_DB.Create(&records).Error)
			require.NoError(t, model.LOG_DB.Create(&model.Log{CreatedAt: cutoff - 1, Content: "retained usage"}).Error)
			operation := service.VerificationOperation{Scope: service.VerificationScopeAuditLogClear, Context: []byte(fmt.Sprintf(`{"target_timestamp":%d}`, cutoff))}
			proofIdentity := identity
			if scenario == "wrong scope" {
				operation.Scope, operation.Context = service.VerificationScopePasswordChange, nil
			}
			if scenario == "wrong cutoff" {
				operation.Context = []byte(fmt.Sprintf(`{"target_timestamp":%d}`, cutoff-10))
			}
			if scenario == "other session" {
				bundle, err := service.CreateLoginSession(user.Id, "password", "127.0.0.1", "second-session")
				require.NoError(t, err)
				proofIdentity, err = service.ParseAccessToken(bundle.AccessToken)
				require.NoError(t, err)
			}
			proof := ""
			if scenario != "missing" && scenario != "admin" && scenario != "access token" {
				proof = issueSecurityEnrollmentProof(t, proofIdentity, operation, service.VerificationMethodPassword)
			}
			if scenario == "expired" {
				require.NoError(t, model.DB.Model(&model.AuthFlow{}).Where("purpose = ?", model.AuthFlowPurposeSecurityProof).Update("expires_at", time.Now().Add(-time.Minute)).Error)
			}
			if scenario == "consumed" {
				_, err := service.ConsumeOperationProof(proof, identity, operation)
				require.NoError(t, err)
			}
			response := securityEnrollmentRequest(http.MethodPost, "/api/log/audit/cleanup", fmt.Sprintf(`{"target_timestamp":%d}`, cutoff), proof, identity, func(c *gin.Context) {
				c.Set("role", common.RoleRootUser)
				if scenario == "admin" {
					c.Set("role", common.RoleAdminUser)
				}
				if scenario == "access token" {
					c.Set("use_access_token", true)
				}
				ClearAuditLogs(c)
			})
			var older, preserved, usage int64
			require.NoError(t, model.LOG_DB.Model(&model.AuditLog{}).Where("created_at < ?", cutoff).Count(&older).Error)
			require.NoError(t, model.LOG_DB.Model(&model.AuditLog{}).Where("event_id IN ?", []string{"at-cutoff", "newer"}).Count(&preserved).Error)
			require.NoError(t, model.LOG_DB.Model(&model.Log{}).Count(&usage).Error)
			assert.EqualValues(t, 2, preserved)
			assert.EqualValues(t, 1, usage)
			if scenario == "success" {
				assert.Equal(t, http.StatusOK, response.Code)
				assert.Contains(t, response.Body.String(), `"deleted_count":2`)
				assert.Zero(t, older)
				var event model.AuditLog
				require.NoError(t, model.LOG_DB.Where("action = ?", "audit_log.clear").First(&event).Error)
				assert.True(t, event.Success)
				assert.Equal(t, model.AuditCategorySecurity, event.Category)
				assert.GreaterOrEqual(t, event.CreatedAt, cutoff)
				require.NotNil(t, event.Other.Op)
				metadata, err := common.Marshal(event.Other.Op.Params)
				require.NoError(t, err)
				assert.JSONEq(t, fmt.Sprintf(`{"target_timestamp":%d,"deleted_count":2}`, cutoff), string(metadata))
				replayed := securityEnrollmentRequest(http.MethodPost, "/api/log/audit/cleanup", fmt.Sprintf(`{"target_timestamp":%d}`, cutoff), proof, identity, func(c *gin.Context) {
					c.Set("role", common.RoleRootUser)
					ClearAuditLogs(c)
				})
				assert.Equal(t, http.StatusForbidden, replayed.Code)
				assert.Contains(t, replayed.Body.String(), "SECURITY_PROOF_CONSUMED")
			} else {
				assert.Equal(t, http.StatusForbidden, response.Code)
				assert.EqualValues(t, 2, older)
			}
		})
	}
}

func TestAuditCleanupFailureConsumesProofAndRollsBack(t *testing.T) {
	user, identity := setupSecurityEnrollmentTest(t)
	require.NoError(t, model.DB.Model(user).Update("role", common.RoleRootUser).Error)
	cutoff := time.Now().Add(-time.Hour).Unix()
	require.NoError(t, model.LOG_DB.Create(&model.AuditLog{EventId: "retained-on-failure", CreatedAt: cutoff - 1}).Error)
	operation := service.VerificationOperation{Scope: service.VerificationScopeAuditLogClear, Context: []byte(fmt.Sprintf(`{"target_timestamp":%d}`, cutoff))}
	proof := issueSecurityEnrollmentProof(t, identity, operation, service.VerificationMethodPassword)
	require.NoError(t, model.LOG_DB.Callback().Create().Before("gorm:create").Register("cleanup_completion_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "audit_logs" {
			tx.AddError(fmt.Errorf("completion insert unavailable"))
		}
	}))
	handler := func(c *gin.Context) { c.Set("role", common.RoleRootUser); ClearAuditLogs(c) }
	response := securityEnrollmentRequest(http.MethodPost, "/api/log/audit/cleanup", fmt.Sprintf(`{"target_timestamp":%d}`, cutoff), proof, identity, handler)
	assert.Contains(t, response.Body.String(), `"success":false`)
	var retained int64
	require.NoError(t, model.LOG_DB.Model(&model.AuditLog{}).Where("event_id = ?", "retained-on-failure").Count(&retained).Error)
	assert.EqualValues(t, 1, retained)
	require.NoError(t, model.LOG_DB.Callback().Create().Remove("cleanup_completion_failure"))
	response = securityEnrollmentRequest(http.MethodPost, "/api/log/audit/cleanup", fmt.Sprintf(`{"target_timestamp":%d}`, cutoff), proof, identity, handler)
	assert.Equal(t, http.StatusForbidden, response.Code)
	assert.Contains(t, response.Body.String(), "SECURITY_PROOF_CONSUMED", "action failure must not restore a spent proof")
}

func TestAuditCleanupFactorPolicy(t *testing.T) {
	for _, scenario := range []string{"password", "password disabled", "twofa", "twofa locked", "passkey disabled", "passwordless without oauth"} {
		t.Run(scenario, func(t *testing.T) {
			user, identity := setupSecurityEnrollmentTest(t)
			require.NoError(t, model.DB.Model(user).Update("role", common.RoleRootUser).Error)
			passwordEnabled := common.PasswordLoginEnabled
			common.PasswordLoginEnabled = scenario != "password disabled"
			t.Cleanup(func() { common.PasswordLoginEnabled = passwordEnabled })
			expected := service.VerificationMethodPassword
			available := true
			switch scenario {
			case "password disabled":
				available = false
			case "twofa", "twofa locked":
				twoFA := model.TwoFA{UserId: user.Id, Secret: "JBSWY3DPEHPK3PXP", IsEnabled: true}
				if scenario == "twofa locked" {
					until := time.Now().Add(time.Minute)
					twoFA.LockedUntil, available = &until, false
				}
				require.NoError(t, model.DB.Create(&twoFA).Error)
				expected = service.VerificationMethodTwoFA
			case "passkey disabled":
				require.NoError(t, model.DB.Create(&model.PasskeyCredential{UserID: user.Id, CredentialID: "cleanup-passkey", PublicKey: "public-key"}).Error)
				system_setting.GetPasskeySettings().Enabled = false
				expected, available = service.VerificationMethodPasskey, false
			case "passwordless without oauth":
				require.NoError(t, model.DB.Model(user).Update("password", "").Error)
				expected, available = service.VerificationMethodOAuth, false
			}
			requirements, err := service.GetVerificationRequirements(identity, service.VerificationScopeAuditLogClear)
			require.NoError(t, err)
			require.Len(t, requirements.Methods, 1)
			assert.Equal(t, expected, requirements.Methods[0].Method)
			assert.Equal(t, available, requirements.Methods[0].Available)
			if scenario != "password" {
				_, err = service.RequireVerificationMethod(identity, service.VerificationScopeAuditLogClear, service.VerificationMethodPassword)
				assert.Error(t, err, "locked or disabled enrolled factors must never fall back to password")
			}
		})
	}
}

func TestAuditCleanupClickHouse(t *testing.T) {
	dsn := os.Getenv("AUDIT_CLICKHOUSE_DSN")
	if dsn == "" {
		t.Skip("AUDIT_CLICKHOUSE_DSN is not configured")
	}
	user, identity := setupSecurityEnrollmentTest(t)
	require.NoError(t, model.DB.Model(user).Update("role", common.RoleRootUser).Error)
	logDB, _ := newAuditTestDatabase(t, "clickhouse", dsn)
	previousLogDB, previousType := model.LOG_DB, common.LogDatabaseType()
	model.LOG_DB = logDB
	common.SetLogDatabaseType(common.DatabaseTypeClickHouse)
	t.Cleanup(func() { model.LOG_DB = previousLogDB; common.SetLogDatabaseType(previousType) })
	require.NoError(t, model.MigrateAuditLogs())
	require.NoError(t, logDB.Exec(releasedClickHouseLogSchema).Error)
	cutoff := time.Now().Add(-time.Hour).Unix()
	model.RecordAuditLog(nil, model.AuditLog{UserId: user.Id, Username: user.Username, ActorRole: common.RoleRootUser, EventId: "cleanup-old", CreatedAt: cutoff - 1})
	model.RecordAuditLog(nil, model.AuditLog{UserId: user.Id, Username: user.Username, ActorRole: common.RoleRootUser, EventId: "cleanup-boundary", CreatedAt: cutoff})
	require.NoError(t, logDB.Exec("INSERT INTO logs (created_at, content) VALUES (?, ?)", cutoff-1, "usage retained").Error)
	mode := operation_setting.AdminAuditMode
	operation_setting.AdminAuditMode = operation_setting.AdminAuditModeBillingOnly
	t.Cleanup(func() { operation_setting.AdminAuditMode = mode })
	operation := service.VerificationOperation{Scope: service.VerificationScopeAuditLogClear, Context: []byte(fmt.Sprintf(`{"target_timestamp":%d}`, cutoff))}
	proof := issueSecurityEnrollmentProof(t, identity, operation, service.VerificationMethodPassword)
	response := securityEnrollmentRequest(http.MethodPost, "/api/log/audit/cleanup", string(operation.Context), proof, identity, func(c *gin.Context) { c.Set("role", common.RoleRootUser); ClearAuditLogs(c) })
	assert.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), `"deleted_count":1`)
	entries, count, err := model.GetAuditLogs(model.AuditLogFilter{}, 0, 20, common.RoleRootUser)
	require.NoError(t, err)
	assert.EqualValues(t, 2, count)
	assert.Equal(t, "audit_log.clear", entries[0].Action)
	assert.GreaterOrEqual(t, entries[0].CreatedAt, cutoff)
	require.NotNil(t, entries[0].Other.Op)
	metadata, err := common.Marshal(entries[0].Other.Op.Params)
	require.NoError(t, err)
	assert.JSONEq(t, fmt.Sprintf(`{"target_timestamp":%d,"deleted_count":1}`, cutoff), string(metadata))
	var usage int64
	require.NoError(t, logDB.Model(&model.Log{}).Count(&usage).Error)
	assert.EqualValues(t, 1, usage)

	// Once a synchronous mutation finishes, client cancellation must not
	// cancel the mandatory completion insert into the separate ClickHouse DB.
	model.RecordAuditLog(nil, model.AuditLog{UserId: user.Id, Username: user.Username, ActorRole: common.RoleRootUser, EventId: "cleanup-cancel-old", CreatedAt: cutoff - 1})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, logDB.Callback().Raw().After("gorm:raw").Register("cleanup_cancel_after_mutation", func(tx *gorm.DB) { cancel() }))
	deleted, err := model.ClearAuditLogsBeforeWithAudit(ctx, cutoff, model.AuditLog{UserId: user.Id, Username: user.Username})
	require.NoError(t, logDB.Callback().Raw().Remove("cleanup_cancel_after_mutation"))
	require.NoError(t, err)
	assert.EqualValues(t, 1, deleted)
	assert.ErrorIs(t, ctx.Err(), context.Canceled)
	var completions int64
	require.NoError(t, logDB.Model(&model.AuditLog{}).Where("action = ?", "audit_log.clear").Count(&completions).Error)
	assert.EqualValues(t, 2, completions)

	model.RecordAuditLog(nil, model.AuditLog{UserId: user.Id, Username: user.Username, ActorRole: common.RoleRootUser, EventId: "cleanup-write-failure-old", CreatedAt: cutoff - 1})
	require.NoError(t, logDB.Callback().Create().Before("gorm:create").Register("cleanup_fail_completion", func(tx *gorm.DB) { tx.AddError(fmt.Errorf("completion insert unavailable")) }))
	deleted, err = model.ClearAuditLogsBeforeWithAudit(context.Background(), cutoff, model.AuditLog{UserId: user.Id, Username: user.Username})
	require.NoError(t, logDB.Callback().Create().Remove("cleanup_fail_completion"))
	assert.EqualValues(t, 1, deleted)
	assert.ErrorContains(t, err, "cleanup completed but recording its completion failed", "ClickHouse cannot roll back a finished mutation")
}

func TestAuditCleanupRejectsInvalidCutoff(t *testing.T) {
	user, identity := setupSecurityEnrollmentTest(t)
	for _, cutoff := range []int64{0, -1, time.Now().Add(time.Hour).Unix()} {
		operation := service.VerificationOperation{Scope: service.VerificationScopeAuditLogClear, Context: []byte(fmt.Sprintf(`{"target_timestamp":%d}`, cutoff))}
		_, err := service.BindVerificationOperation(operation)
		assert.ErrorIs(t, err, service.ErrVerificationContextInvalid)
		_, err = model.ClearAuditLogsBefore(context.Background(), cutoff)
		assert.Error(t, err)
	}
	_, err := service.GetVerificationRequirements(identity, service.VerificationScopeAuditLogClear)
	assert.ErrorIs(t, err, service.ErrVerificationForbidden)
	user.Role = common.RoleRootUser
	require.NoError(t, model.DB.Model(user).Update("role", user.Role).Error)
	methods, err := service.GetVerificationRequirements(identity, service.VerificationScopeAuditLogClear)
	require.NoError(t, err)
	require.Len(t, methods.Methods, 1)
	assert.Equal(t, service.VerificationMethodPassword, methods.Methods[0].Method)
}
