package authz

// Redemption resource and its actions.
const ResourceRedemption = "redemption"

// Permission helpers for the redemption resource.
var (
	RedemptionRead   = Permission{Resource: ResourceRedemption, Action: ActionRead}
	RedemptionWrite  = Permission{Resource: ResourceRedemption, Action: ActionWrite}
	RedemptionDelete = Permission{Resource: ResourceRedemption, Action: ActionDelete}
)

func init() {
	RegisterResource(ResourceDefinition{
		Resource: ResourceRedemption,
		LabelKey: "Redemption Management",
		Actions: []ActionDefinition{
			{
				Action:         ActionRead,
				LabelKey:       "Read redemption codes",
				DescriptionKey: "View redemption code lists and details.",
				DefaultRoles:   []string{BuiltInRoleAdmin},
			},
			{
				Action:         ActionWrite,
				LabelKey:       "Edit redemption codes",
				DescriptionKey: "Generate and edit redemption codes.",
				DefaultRoles:   []string{BuiltInRoleAdmin},
			},
			{
				Action:         ActionDelete,
				LabelKey:       "Delete redemption codes",
				DescriptionKey: "Delete redemption codes, including invalid ones.",
				DefaultRoles:   []string{BuiltInRoleAdmin},
			},
		},
	})
}
