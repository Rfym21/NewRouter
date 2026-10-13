package authz

// Log resource and its actions.
//
// export_manage(日志导出的全站管理:列出/批量删除/延期/下载/分享他人导出)默认不授予任何角色,
// 仅 root 自动拥有,可由 root 下放给指定管理员。用户对自己导出的自助操作走 UserAuth,不在此建模。
const ResourceLog = "log"

// Permission helpers for the log resource.
var (
	LogRead         = Permission{Resource: ResourceLog, Action: ActionRead}
	LogExportManage = Permission{Resource: ResourceLog, Action: ActionExportManage}
)

func init() {
	RegisterResource(ResourceDefinition{
		Resource: ResourceLog,
		LabelKey: "Log Management",
		Actions: []ActionDefinition{
			{
				Action:         ActionRead,
				LabelKey:       "Read logs",
				DescriptionKey: "View all-user logs, statistics, and search.",
				DefaultRoles:   []string{BuiltInRoleAdmin},
			},
			{
				Action:         ActionExportManage,
				LabelKey:       "Manage log exports",
				DescriptionKey: "List, batch-delete, extend, download, and share other users' log exports.",
			},
		},
	})
}
