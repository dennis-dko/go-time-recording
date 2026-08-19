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

// IsDefault reports whether this is one of the roles a fresh installation is
// seeded with.
//
// A separate question from IsSystem, and the two are separate on purpose.
//
// IsSystem is about the installation being able to administer itself: the admin
// role cannot be deleted, renamed, or stripped of a right, because an
// installation that loses it loses the way back into its own settings.
//
// This one is about not throwing away furniture. The other shipped roles - the
// ordinary one, and the one that spans both jobs - are ordinary roles: what they
// grant is an installation's business and can be edited. What should not happen
// is one of them being deleted, because they are what every account is assigned
// on arrival and what the directory synchronisation assigns by default. Recreating
// one by hand means getting a list of permissions exactly right from memory.
//
// So: the defaults cannot be deleted, and only the admin role is otherwise fixed.
func (r *Role) IsDefault() bool {
	for _, shipped := range DefaultRoles() {
		if shipped.Name == r.Name {
			return true
		}
	}

	return false
}
