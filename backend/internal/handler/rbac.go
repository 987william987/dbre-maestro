package handler

import "github.com/dbre-maestro/maestro/internal/model"

type authGroupMeta struct {
	Name        model.AuthGroup `json:"name"`
	Label       string          `json:"label"`
	Description string          `json:"description"`
	System      bool            `json:"system_defined"`
}

var authGroupCatalog = []authGroupMeta{
	{
		Name:        model.AuthGroupDeveloper,
		Label:       "Developer",
		Description: "Can create tickets and use SQL editor within granted resources.",
		System:      true,
	},
	{
		Name:        model.AuthGroupDBA,
		Label:       "DBA",
		Description: "Can manage database connections, query execution, and governance settings.",
		System:      true,
	},
	{
		Name:        model.AuthGroupAdmin,
		Label:       "Admin",
		Description: "Has full platform access including user and RBAC administration.",
		System:      true,
	},
	{
		Name:        model.AuthGroupSecurity,
		Label:       "Security",
		Description: "Reviews sensitive data workflows and data export requests.",
		System:      true,
	},
	{
		Name:        model.AuthGroupDataOwner,
		Label:       "Data Owner",
		Description: "Reviews regular database change and access tickets for owned data scope.",
		System:      true,
	},
}

func authGroupMetadata(group model.AuthGroup) (authGroupMeta, bool) {
	for _, item := range authGroupCatalog {
		if item.Name == group {
			return item, true
		}
	}
	return authGroupMeta{}, false
}

func isValidAuthGroup(group model.AuthGroup) bool {
	_, ok := authGroupMetadata(group)
	return ok
}

func protectedUserErrorMessage() string {
	return "protected system user cannot be modified"
}
