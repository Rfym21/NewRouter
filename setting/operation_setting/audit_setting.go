package operation_setting

import "sync/atomic"

const AuditLogEnabledOptionKey = "AuditLogEnabled"

// Store the disabled state so the zero value preserves existing installations'
// auditing. Reads on request paths and option hot updates are concurrency-safe.
var auditLogDisabled atomic.Bool

func IsAuditLogEnabled() bool {
	return !auditLogDisabled.Load()
}

func SetAuditLogEnabled(enabled bool) {
	auditLogDisabled.Store(!enabled)
}

// 管理写操作审计模式。
//
// billing_only(默认):不额外兜底记录管理端写操作，已有明确埋点的审计事件仍可记录。
// full_audit:鉴权链路对所有经过 AdminAuth/RootAuth 的写操作(POST/PUT/PATCH/DELETE)
// 兜底留痕(model.RecordOperationAuditLog)。
// 两种模式均受 AuditLogEnabled 总开关控制；计费业务日志(model.Log)不受影响。
//
// 该项通过 option 表持久化并热更新(见 model/option.go 的 InitOptionMap / updateOptionMap),
// 仅 root 可通过 /api/option 修改。
const (
	AdminAuditModeBillingOnly = "billing_only"
	AdminAuditModeFull        = "full_audit"
)

// AdminAuditMode 当前审计模式,默认 billing_only。由 updateOptionMap 在配置变更时同步。
var AdminAuditMode = AdminAuditModeBillingOnly

// IsFullAdminAudit reports whether full admin write-operation auditing is enabled.
func IsFullAdminAudit() bool {
	return IsAuditLogEnabled() && AdminAuditMode == AdminAuditModeFull
}
