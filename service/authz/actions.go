package authz

// 跨资源复用的通用动作常量。read / write / operate / sensitive_write / secret_view
// 已在 resources_channel.go 定义,这里补充其余资源域需要的动作。
const (
	// ActionManage 表示高危状态管理(如封禁、改额度、重置凭证),默认仅 root。
	ActionManage = "manage"
	// ActionDelete 表示删除类操作。
	ActionDelete = "delete"
	// ActionExportManage 表示对全站导出产物的管理,默认仅 root。
	ActionExportManage = "export_manage"
)
