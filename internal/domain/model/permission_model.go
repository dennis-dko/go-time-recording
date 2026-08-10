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

	// There is no projects:write:own. It allowed creating and managing a private
	// category, as opposed to projects:write for a shared project - two rights for two
	// kinds of project.
	//
	// There is one kind now: every project belongs to one person, so every project is
	// what the "own" right used to be about. Keeping both would have meant one of them
	// granting nothing, and which one would have depended on where you looked.

	// Own vs. all separates "my time sheet" from "everyone's".
	PermTimesheetReadOwn  = "timesheets:read:own"
	PermTimesheetReadAll  = "timesheets:read:all"
	PermTimesheetWriteOwn = "timesheets:write:own"
	PermTimesheetWriteAll = "timesheets:write:all"
	PermTimesheetTransfer = "timesheets:transfer"
	PermReportReadOwn     = "reports:read:own"
	PermSettingsWriteOwn  = "settings:write:own"
)

// There is no settings:write:other. A daily target and a daily ceiling are time
// figures, and everything to do with time is the person's own.
//
// It existed so the administrator could set them for somebody else, and the reason
// that does not work is in this same file: the administrator cannot read that
// person's entries, their balance or their figures. Setting a number whose effect is
// invisible to you is not administration, it is guessing - and the number's only
// consumer is the overtime balance, which nobody but its owner may see anyway.
//
// It was also not the lock it looked like. The same two fields were writable through
// PUT /users/{id} and through the spreadsheet import, both of which check only
// users:write, so the right guarded one of three doors.
//
// What stays with the administrator is the instance-wide default under Settings,
// which is what a new account gets until its owner decides otherwise.

// Whether somebody may see another person's recorded time is one question, and
// PermTimesheetReadAll is the one right that answers it.
//
// There used to be a second, reports:read, for the same question asked of a total
// rather than of a list - and it went to the role that reviewed other people's
// hours. That role is gone, and with it the idea that anybody reviews anybody:
// everyone keeps their own. What was left was a right no role held, gating a whole
// screen that therefore nobody could reach, and a second answer to a question that
// should only have one - which is how two answers come to disagree.
//
// So every read that could show another person's time now checks the same right:
// the entry list, the spreadsheet export, a project's total, and somebody else's
// overtime balance. No default role holds it.

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
		PermTimesheetTransfer,
		PermReportReadOwn,
		PermSettingsWriteOwn,
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
// time like anybody else. Nobody else's hours, and no total over them either:
// everyone keeps their own, and nobody sees what anybody else has. That is the whole
// arrangement now that there is no role between this one and an ordinary account.
//
// This is not a hint. The rights are enforced per endpoint, so the administrator
// is refused these by the same code that refuses an employee.
func SystemAdminPermissions() []string {
	return []string{
		PermUserRead, PermUserWrite, PermUserDelete,
		PermRoleRead, PermRoleWrite,

		// Its own projects, whole: there is one kind of project now and it belongs to
		// one person, so keeping one means being able to finish it and remove it too.
		// This used to read "the shared projects, not managing them", from when there
		// were two kinds - and the own-project right it held then already allowed
		// removing your own, so this is the same reach said in the new vocabulary.
		PermProjectRead, PermProjectWrite,
		PermProjectArchive, PermProjectDelete,

		// An administrator is also a person who works here.
		PermTimesheetReadOwn, PermTimesheetWriteOwn, PermReportReadOwn,

		// Their own preferences, and nobody else's: the daily target and the ceiling
		// belong to whoever they are about.
		PermSettingsWriteOwn,
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
			Name:        RoleEmployee,
			Description: "Keeps their own time, projects and calendar",
			// Everything about their own work, and nothing about anybody else's.
			// There is no third role between this one and the administrator: a
			// manager who approved other people's hours needed a review path, and
			// there is none - everyone keeps their own.
			//
			// That includes the projects. They are the person's own categories, so
			// creating, completing, archiving and deleting one is theirs to do;
			// PermProjectRead still only shows what they may see.
			Permissions: []string{
				PermProjectRead, PermProjectWrite,
				PermProjectArchive, PermProjectDelete,
				PermTimesheetReadOwn, PermTimesheetWriteOwn, PermTimesheetTransfer,
				PermReportReadOwn, PermSettingsWriteOwn,
			},
		},
	}
}
