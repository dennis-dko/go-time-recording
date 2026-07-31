package model

import "slices"

// Role is a named set of permissions.
type Role struct {
	ID          uint
	Name        string
	Description string

	// IsSystem marks a role the application depends on. System roles cannot
	// be deleted or renamed, and their permissions cannot be reduced.
	IsSystem bool

	Permissions []string
}

// Has reports whether the role grants the permission.
func (r *Role) Has(permission string) bool {
	return slices.Contains(r.Permissions, permission)
}
