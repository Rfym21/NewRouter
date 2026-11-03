package operation_setting

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAuditLoggingDefaultsEnabledAndControlsFullAdminAudit(t *testing.T) {
	assert.True(t, IsAuditLogEnabled(), "upgrades retain audit logging")
	previousMode := AdminAuditMode
	t.Cleanup(func() { SetAuditLogEnabled(true); AdminAuditMode = previousMode })
	AdminAuditMode = AdminAuditModeFull
	assert.True(t, IsFullAdminAudit())
	SetAuditLogEnabled(false)
	assert.False(t, IsFullAdminAudit())
	assert.Equal(t, AdminAuditModeFull, AdminAuditMode, "the selected audit scope is retained")
	SetAuditLogEnabled(true)
	assert.True(t, IsFullAdminAudit())
}

func TestAuditLoggingConcurrentReadsAndUpdates(t *testing.T) {
	previousEnabled := IsAuditLogEnabled()
	t.Cleanup(func() { SetAuditLogEnabled(previousEnabled) })
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			for j := range 1000 {
				SetAuditLogEnabled((i+j)%2 == 0)
				_ = IsAuditLogEnabled()
			}
		})
	}
	wg.Wait()
	SetAuditLogEnabled(false)
	assert.False(t, IsAuditLogEnabled())
	SetAuditLogEnabled(true)
	assert.True(t, IsAuditLogEnabled())
}
