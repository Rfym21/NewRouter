package authz

// Model resource and its actions.
//
// 覆盖模型元数据管理(/api/models 的增删改查与上游同步)。模型定价/倍率仍走 option 系统
// 的 RootAuth,不在此建模。
const ResourceModel = "model"

// Permission helpers for the model resource.
var (
	ModelRead  = Permission{Resource: ResourceModel, Action: ActionRead}
	ModelWrite = Permission{Resource: ResourceModel, Action: ActionWrite}
)

func init() {
	RegisterResource(ResourceDefinition{
		Resource: ResourceModel,
		LabelKey: "Model Management",
		Actions: []ActionDefinition{
			{
				Action:         ActionRead,
				LabelKey:       "Read models",
				DescriptionKey: "View model metadata, missing models, and upstream sync preview.",
				DefaultRoles:   []string{BuiltInRoleAdmin},
			},
			{
				Action:         ActionWrite,
				LabelKey:       "Edit models",
				DescriptionKey: "Create/edit/delete model metadata and sync from upstream.",
				DefaultRoles:   []string{BuiltInRoleAdmin},
			},
		},
	})
}
