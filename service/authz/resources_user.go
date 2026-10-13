package authz

// User resource and its actions.
//
// manage(封禁/启用/提升/改额度/重置凭证等高危操作)与 delete(删除/清除用户)默认不授予
// 任何角色,即仅 root 自动拥有,可由 root 通过用户级 override 下放给指定管理员。
// 用户「权限点分配」接口不在此建模,始终保持 RootAuth 硬门槛,避免被授权管理员自我提权。
const ResourceUser = "user"

// Permission helpers for the user resource.
var (
	UserRead   = Permission{Resource: ResourceUser, Action: ActionRead}
	UserWrite  = Permission{Resource: ResourceUser, Action: ActionWrite}
	UserManage = Permission{Resource: ResourceUser, Action: ActionManage}
	UserDelete = Permission{Resource: ResourceUser, Action: ActionDelete}
)

func init() {
	RegisterResource(ResourceDefinition{
		Resource: ResourceUser,
		LabelKey: "User Management",
		Actions: []ActionDefinition{
			{
				Action:         ActionRead,
				LabelKey:       "Read users",
				DescriptionKey: "View user lists, details, top-up records, and 2FA stats.",
				DefaultRoles:   []string{BuiltInRoleAdmin},
			},
			{
				Action:         ActionWrite,
				LabelKey:       "Edit users",
				DescriptionKey: "Create and edit user profiles and settings.",
				DefaultRoles:   []string{BuiltInRoleAdmin},
			},
			{
				Action:         ActionManage,
				LabelKey:       "Manage user state",
				DescriptionKey: "Ban, promote, adjust quota, complete top-ups, and reset passkeys/2FA/bindings.",
			},
			{
				Action:         ActionDelete,
				LabelKey:       "Delete users",
				DescriptionKey: "Delete users and purge soft-deleted accounts.",
			},
		},
	})
}
