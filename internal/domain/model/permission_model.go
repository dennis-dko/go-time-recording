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

	// PermProjectWriteOwn allows creating and managing private projects,
	// which act as personal categories and are visible only to their owner.
	PermProjectWriteOwn = "projects:write:own"

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
		PermProjectWriteOwn,
		PermTimesheetReadOwn, PermTimesheetReadAll,
		PermTimesheetWriteOwn, PermTimesheetWriteAll,
		PermTimesheetApprove, PermTimesheetTransfer,
		PermReportRead, PermReportReadOwn,
		PermSettingsWriteOwn, PermSettingsWriteOther,
	}
}

// SystemAdminPermissions is what the built-in administrator holds.
//
// Deliberately not AllPermissions(). Administering an installation and reading
// what people recorded in it are two different jobs, and the account that exists
// on every installation is the one least entitled to the second: nobody chose to
// give it that reach, it arrived with the software. An administrator restoring a
// backup or repointing the directory has no business in a colleague's week.
//
// So it manages the installation, its users and their roles, and books its own
// time like anybody else. Everyone else's hours, the approvals, the transfers and
// the reports belong to whoever the organisation puts in charge of the work - the
// manager role by default, and any role it chooses to define.
//
// This is not a hint. The rights are enforced per endpoint, so the administrator
// is refused these by the same code that refuses an employee.
func SystemAdminPermissions() []string {
	return []string{
		PermUserRead, PermUserWrite, PermUserDelete,
		PermRoleRead, PermRoleWrite,

		// Reading the shared projects, not managing them: time has to be bookable
		// against something, and that list is what every employee sees anyway.
		// Creating and archiving projects is running the work.
		PermProjectRead, PermProjectWriteOwn,

		// An administrator is also a person who works here.
		PermTimesheetReadOwn, PermTimesheetWriteOwn, PermReportReadOwn,

		// Their own preferences, and the per-account figures that are part of
		// administering an account rather than of reading it: the daily target and
		// the ceiling.
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
			Description: "Administers the installation, its users and their roles",
			IsSystem:    true,
			Permissions: SystemAdminPermissions(),
		},
		{
			Name:        RoleManager,
			Description: "Manages projects, approves time entries and reads the reports",
			// PermReportRead lives here rather than with the administrator. It was
			// withheld from this role while the administrator held it, on the
			// reasoning that one account alone should see what people total up to;
			// with the administrator out of everyone else's hours that reasoning
			// points the other way. A manager already reads every entry one by one
			// in order to approve it, so the total of those entries reveals nothing
			// further - and left with nobody, the report would be a screen no
			// installation could open.
			Permissions: []string{
				PermUserRead, PermRoleRead,
				PermProjectRead, PermProjectWrite, PermProjectArchive, PermProjectWriteOwn,
				// Refused while a project still has entries, so this deletes an
				// empty project somebody created by mistake rather than anyone's
				// recorded time.
				PermProjectDelete,
				PermTimesheetReadOwn, PermTimesheetReadAll,
				PermTimesheetWriteOwn, PermTimesheetWriteAll,
				PermTimesheetApprove, PermTimesheetTransfer,
				PermReportRead, PermReportReadOwn,
				PermSettingsWriteOwn, PermSettingsWriteOther,
			},
		},
		{
			Name:        RoleEmployee,
			Description: "Books and submits their own time",
			Permissions: []string{
				PermProjectRead, PermProjectWriteOwn,
				PermTimesheetReadOwn, PermTimesheetWriteOwn,
				PermReportReadOwn, PermSettingsWriteOwn,
			},
		},
	}
}
