package model

// Permissions are the atomic rights a role can hold. They are constants rather
// than database rows because each one is enforced by a specific line of code:
// a permission that exists only in the database would grant nothing.
const (
	PermUserRead   = "users:read"
	PermUserWrite  = "users:write"
	PermUserDelete = "users:delete"

	PermRoleRead  = "roles:read"
	PermRoleWrite = "roles:write"

	PermProjectRead    = "projects:read"
	PermProjectWrite   = "projects:write"
	PermProjectDelete  = "projects:delete"
	PermProjectArchive = "projects:archive"

	// Own vs. all separates "my time sheet" from "everyone's".
	PermTimesheetReadOwn   = "timesheets:read:own"
	PermTimesheetReadAll   = "timesheets:read:all"
	PermTimesheetWriteOwn  = "timesheets:write:own"
	PermTimesheetWriteAll  = "timesheets:write:all"
	PermTimesheetApprove   = "timesheets:approve"
	PermTimesheetTransfer  = "timesheets:transfer"
	PermReportRead         = "reports:read"
	PermReportReadOwn      = "reports:read:own"
	PermSettingsWriteOwn   = "settings:write:own"
	PermSettingsWriteOther = "settings:write:other"
)

// AllPermissions lists every permission the application enforces. The role
// administration UI offers exactly these, so a typo cannot create a permission
// that no code ever checks.
func AllPermissions() []string {
	return []string{
		PermUserRead, PermUserWrite, PermUserDelete,
		PermRoleRead, PermRoleWrite,
		PermProjectRead, PermProjectWrite, PermProjectDelete, PermProjectArchive,
		PermTimesheetReadOwn, PermTimesheetReadAll,
		PermTimesheetWriteOwn, PermTimesheetWriteAll,
		PermTimesheetApprove, PermTimesheetTransfer,
		PermReportRead, PermReportReadOwn,
		PermSettingsWriteOwn, PermSettingsWriteOther,
	}
}

// IsPermission reports whether name is a permission this application enforces.
func IsPermission(name string) bool {
	for _, p := range AllPermissions() {
		if p == name {
			return true
		}
	}

	return false
}

// Default role names created on first start.
const (
	RoleAdmin    = "admin"
	RoleManager  = "manager"
	RoleEmployee = "employee"
)

// DefaultRoles is the role set seeded on first start. Admin is marked as a
// system role: it cannot be deleted or stripped of its permissions, otherwise
// an installation could be locked out of its own administration.
func DefaultRoles() []Role {
	return []Role{
		{
			Name:        RoleAdmin,
			Description: "Full administrative access",
			IsSystem:    true,
			Permissions: AllPermissions(),
		},
		{
			Name:        RoleManager,
			Description: "Manages projects and approves time entries",
			// Deliberately without PermReportRead: only the built-in
			// administrator may see what other people total up to.
			Permissions: []string{
				PermUserRead, PermRoleRead,
				PermProjectRead, PermProjectWrite, PermProjectArchive,
				PermTimesheetReadOwn, PermTimesheetReadAll,
				PermTimesheetWriteOwn, PermTimesheetWriteAll,
				PermTimesheetApprove, PermTimesheetTransfer,
				PermReportReadOwn,
				PermSettingsWriteOwn, PermSettingsWriteOther,
			},
		},
		{
			Name:        RoleEmployee,
			Description: "Books and submits their own time",
			Permissions: []string{
				PermProjectRead,
				PermTimesheetReadOwn, PermTimesheetWriteOwn,
				PermReportReadOwn, PermSettingsWriteOwn,
			},
		},
	}
}
