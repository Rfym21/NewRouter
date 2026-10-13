package authz

// Subscription resource and its actions.
//
// delete(作废/删除用户订阅)默认不授予任何角色,仅 root 自动拥有,可由 root 下放。
const ResourceSubscription = "subscription"

// Permission helpers for the subscription resource.
var (
	SubscriptionRead   = Permission{Resource: ResourceSubscription, Action: ActionRead}
	SubscriptionWrite  = Permission{Resource: ResourceSubscription, Action: ActionWrite}
	SubscriptionDelete = Permission{Resource: ResourceSubscription, Action: ActionDelete}
)

func init() {
	RegisterResource(ResourceDefinition{
		Resource: ResourceSubscription,
		LabelKey: "Subscription Management",
		Actions: []ActionDefinition{
			{
				Action:         ActionRead,
				LabelKey:       "Read subscriptions",
				DescriptionKey: "View subscription plans and user subscriptions.",
				DefaultRoles:   []string{BuiltInRoleAdmin},
			},
			{
				Action:         ActionWrite,
				LabelKey:       "Edit subscriptions",
				DescriptionKey: "Create/edit plans, bind subscriptions, and reset plan or user subscriptions.",
				DefaultRoles:   []string{BuiltInRoleAdmin},
			},
			{
				Action:         ActionDelete,
				LabelKey:       "Delete subscriptions",
				DescriptionKey: "Invalidate or delete user subscriptions.",
			},
		},
	})
}
