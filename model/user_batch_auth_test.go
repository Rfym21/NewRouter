package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"new-router/common"
)

func TestBatchUserActionsFenceExistingSessions(t *testing.T) {
	for _, action := range []struct {
		name string
		run  func(BatchUserActionFilter) (int64, error)
	}{{"disable", BatchDisableUsersByQuotaFilter}, {"delete", BatchSoftDeleteUsersByQuotaFilter}} {
		t.Run(action.name, func(t *testing.T) {
			truncateTables(t)
			user := User{Username: "batch-" + action.name, AffCode: "batch-" + action.name, Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AuthVersion: 1}
			admin := User{Username: "batch-admin", AffCode: "batch-admin", Role: common.RoleAdminUser, Status: common.UserStatusEnabled, AuthVersion: 1}
			require.NoError(t, DB.Create(&user).Error)
			require.NoError(t, DB.Create(&admin).Error)
			session := UserSession{SID: "batch-session-" + action.name, UserID: user.Id, Version: 1, UserAuthVersion: 1, RefreshHash: "batch-refresh-hash", ExpiresAt: time.Now().Add(time.Hour).Unix()}
			require.NoError(t, CreateUserSession(&session))
			minimum := 0
			affected, err := action.run(BatchUserActionFilter{RemainingQuotaMin: &minimum})
			require.NoError(t, err)
			require.EqualValues(t, 1, affected)
			var stored User
			require.NoError(t, DB.Unscoped().First(&stored, user.Id).Error)
			assert.Greater(t, stored.AuthVersion, int64(1))
			var storedSession UserSession
			require.NoError(t, DB.First(&storedSession, "sid = ?", session.SID).Error)
			assert.Equal(t, UserSessionStatusRevoked, storedSession.Status)
			require.NoError(t, DB.First(&admin, admin.Id).Error)
			assert.Equal(t, common.UserStatusEnabled, admin.Status)
			assert.EqualValues(t, 1, admin.AuthVersion)
			// Restoring the account must not resurrect its prior session.
			require.NoError(t, DB.Unscoped().Model(&User{}).Where("id = ?", user.Id).Updates(map[string]any{"status": common.UserStatusEnabled, "deleted_at": nil}).Error)
			err = DB.Transaction(func(tx *gorm.DB) error {
				return ValidateAuthSessionWithTx(tx, AuthSessionIdentity{UserID: user.Id, SessionID: session.SID, UserAuthVersion: 1, SessionVersion: 1})
			})
			assert.ErrorIs(t, err, ErrUserSessionInactive)
		})
	}
}

func TestRefundSweepRetainsPre2026Eligibility(t *testing.T) {
	truncateTables(t)
	cases := []struct {
		id        string
		submitted int64
		included  bool
	}{
		{"before-local-cutoff", 1740182399, false},
		{"local-cutoff", 1740182400, true},
		{"between-cutoffs", 1750000000, true},
		{"official-cutoff", 1771718400, true},
	}
	for _, tc := range cases {
		task := Task{TaskID: tc.id, SubmitTime: tc.submitted, Status: TaskStatusFailure, Quota: 19}
		insertTask(t, &task)
	}
	found := map[string]bool{}
	for _, task := range GetUnrefundedFailedTasks(time.Now().Unix()+1, 10) {
		found[task.TaskID] = true
	}
	for _, tc := range cases {
		assert.Equal(t, tc.included, found[tc.id], tc.id)
	}
}
