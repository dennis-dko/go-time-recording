package model

import "slices"

// Permissions are the atomic rights a role can hold. They are constants rather
// than database rows because each one is enforced by a specific line of code:
// a permission that exists only in the database would grant nothing.
const (
	PermUserRead   = "users:read"
	PermUserWrite  = "users:write"
	PermUserDelete = "users:delete"

	PermRoleRead  = "roles:read"
	PermRoleWrite = "roles:write"

	// PermSettingsManage opens the installation itself: the database connection,
	// the directory bind, appearance, maintenance mode, telemetry, the process log
	// and the restart.
	//
	// This was deliberately not a permission for a long time, and the argument
	// against it is still true - see Authorizer.RequireInstallationAdmin, which
	// records what granting it costs. It exists because somebody who administers
	// an installation has to be able to administer it without signing in as the
	// built-in account, and holding the accounts but not the configuration is half
	// a job.
	PermSettingsManage = "settings:manage"

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

	// Everything about time is the person's own, so there is no "all" to contrast
	// "own" with any more. The suffix stays on the names because the database and
	// every installation's roles already spell them this way, and renaming a right
	// to say the same thing is a migration that buys nothing.
	PermTimesheetReadOwn  = "timesheets:read:own"
	PermTimesheetWriteOwn = "timesheets:write:own"
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

// There is no timesheets:read:all, and no timesheets:write:all.
//
// They were the last of the manager: one right that opened everybody's entries,
// balances, totals and exports, and one that let somebody book or change time in
// another person's name. Both survived the removal of the role that used them,
// because a right outlives whoever held it, and no default role held either.
//
// A right nobody holds is not the same as a right nobody can hold. These two stayed
// tickable in the role editor, and ticking one changed nothing anybody could see -
// the screens that offered a "which person" choice were gone by then - while the API
// answered every question about every colleague. A capability with no screen is a
// capability nobody audits.
//
// What replaces them is the arrangement itself: whose entry it is decides who may
// read or change it, and that is not a permission because it is not a choice. The
// question "may this person see somebody else's time" now has one answer, no, and
// no box that appears to change it. Somebody who should administer the installation
// as well as record their own hours is given the combined role, which grants the
// accounts and the configuration - never a colleague's time.

// AllPermissions lists every permission the application enforces. The role
// administration UI offers exactly these, so a typo cannot create a permission
// that no code ever checks.
func AllPermissions() []string {
	return []string{
		PermUserRead, PermUserWrite, PermUserDelete,
		PermRoleRead, PermRoleWrite,
		PermSettingsManage,
		PermProjectRead, PermProjectWrite, PermProjectDelete, PermProjectArchive,
		PermTimesheetReadOwn, PermTimesheetWriteOwn,
		PermTimesheetTransfer,
		PermReportReadOwn,
		PermSettingsWriteOwn,
	}
}

// SystemAdminPermissions is what the built-in administrator holds.
//
// Administration, and nothing else. It sets up the installation and it keeps the
// accounts and their roles - that is the whole job, and it does not record time.
//
// It used to book and read its own hours "like anybody else who works here", and that
// was the wrong shape. The account exists on every installation before anybody has
// chosen anything: it is how you get in, not somebody's working day. Whoever does work
// here has an account of their own, and if that person also administers, they are given
// the role below rather than made to sign in twice.
//
// So no time, no projects, no figures, no working times. This is not a hint - the
// rights are enforced per endpoint, so the administrator is refused these by the same
// code that refuses anybody else.
func SystemAdminPermissions() []string {
	return []string{
		PermUserRead, PermUserWrite, PermUserDelete,
		PermRoleRead, PermRoleWrite,
		PermSettingsManage,
	}
}

// UserAdminPermissions is what somebody who works here and also administers holds.
//
// The one arrangement that reaches across the two jobs, and it is granted rather than
// assumed: the built-in administrator hands it out. It is a user's own rights plus the
// administration, which means an ordinary account gaining a second job - not the
// built-in account gaining a working day.
//
// Assembled from the two lists rather than written out, so a right added to either one
// cannot be forgotten here.
func UserAdminPermissions() []string {
	return append(UserPermissions(), SystemAdminPermissions()...)
}

// UserPermissions is what an ordinary account holds: everything about its own work and
// nothing about anybody else's.
func UserPermissions() []string {
	return []string{
		PermProjectRead, PermProjectWrite, PermProjectArchive, PermProjectDelete,
		PermTimesheetReadOwn, PermTimesheetWriteOwn, PermTimesheetTransfer,
		PermReportReadOwn, PermSettingsWriteOwn,
	}
}

// IsPermission reports whether name is a permission this application enforces.
func IsPermission(name string) bool {
	return slices.Contains(AllPermissions(), name)
}

// Default role names created on first start.
//
// The ordinary role was called employee, and its combined one employee-admin. The word
// said more than this application knows: it holds accounts, and whether the person
// behind one is employed here, contracted, a volunteer or the only person in the
// company is not something it records or needs. What it does know is that they use it.
//
// These are identifiers, and the interface does not show them raw - each is translated
// where it is displayed, so a German reader chooses "Benutzer" rather than a lowercase
// English word. An installation that renamed a role sees whatever it typed, which is
// the only sensible answer for words this application has never seen.
const (
	RoleAdmin = "admin"
	RoleUser  = "user"

	// RoleUserAdmin is somebody who works here and also administers.
	//
	// Seeded rather than left to be assembled by hand, because the alternative is an
	// administrator building a role out of two lists of rights and getting one of them
	// slightly wrong - which nobody notices until somebody is refused something, or
	// allowed something.
	RoleUserAdmin = "user-admin"
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
			Name:        RoleUser,
			Description: "Keeps their own time, projects and calendar",
			// Everything about their own work, and nothing about anybody else's -
			// there is no role that reads across accounts, because there is nothing
			// to read across. Projects included: one belongs to this person, so
			// creating, finishing, archiving and removing it is theirs to do.
			Permissions: UserPermissions(),
		},
		{
			Name: RoleUserAdmin,
			Description: "Keeps their own time and projects, and administers the " +
				"installation",
			// The one role that spans both jobs, and the answer to "somebody here
			// needs to administer as well". Given out by the built-in administrator,
			// which is what makes it a decision rather than an accident.
			//
			// Not a system role: an installation that does not want it can remove it,
			// and its rights are visible in the role editor like any other. What must
			// not be removable is the built-in administrator's own role, and that one
			// is marked above.
			Permissions: UserAdminPermissions(),
		},
	}
}
