package operation_setting

// 管理写操作审计模式。
//
// billing_only(默认):仅保留计费业务日志(model.Log),不额外记录管理端写操作审计。
// full_audit:鉴权链路对所有经过 AdminAuth/RootAuth 的写操作(POST/PUT/PATCH/DELETE)
// 兜底留痕(model.RecordOperationAuditLog)。
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
	return AdminAuditMode == AdminAuditModeFull
}
