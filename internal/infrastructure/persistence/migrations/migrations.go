// Package migrations defines the database schema as GoFr migrations, so a
// fresh binary provisions its own database on first start and no external
// schema tooling is needed for deployment.
package migrations

import (
	"fmt"

	"gofr.dev/pkg/gofr/migration"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/sqldb"
)

// All returns the migration set for the given dialect, keyed by version.
//
// The dialect is passed in rather than detected because GoFr's
// migration.Datasource exposes only query methods, not the dialect, and the
// DDL for auto-increment keys and timestamps genuinely differs per engine.
func All(dialect string) map[int64]migration.Migrate {
	return map[int64]migration.Migrate{
		20260730120000: {UP: func(d migration.Datasource) error {
			return execAll(d, createUsers(dialect), createProjects(dialect), createTimesheets(dialect))
		}},
		20260730120100: {UP: func(d migration.Datasource) error {
			return execAll(d,
				"CREATE INDEX idx_timesheets_user_date ON timesheets (user_id, date)",
				"CREATE INDEX idx_timesheets_project ON timesheets (project_id)",
			)
		}},
		20260731010000: {UP: func(d migration.Datasource) error {
			return addRoleBasedAccess(d, dialect)
		}},
		20260731020000: {UP: func(d migration.Datasource) error {
			return addSessionsAndPreferences(d, dialect)
		}},
		20260801010000: {UP: func(d migration.Datasource) error {
			return makeProjectOptional(d, dialect)
		}},
		20260801010100: {UP: func(d migration.Datasource) error {
			return createSettings(d, dialect)
		}},
		20260801020000: {UP: func(d migration.Datasource) error {
			return addPrivateProjects(d, dialect)
		}},
		20260801030000: {UP: func(d migration.Datasource) error {
			return createAPITokens(d, dialect)
		}},
		20260801040000: {UP: func(d migration.Datasource) error {
			return addExternalIdentity(d, dialect)
		}},
		20260801050000: {UP: func(d migration.Datasource) error {
			return addUserTimezone(d, dialect)
		}},
		20260802010000: {UP: func(d migration.Datasource) error {
			return addUserTourSeen(d, dialect)
		}},
		20260803010000: {UP: func(d migration.Datasource) error {
			return createPasskeys(d, dialect)
		}},
		20260805010000: {UP: func(d migration.Datasource) error {
			return createRunningTimers(d, dialect)
		}},
		20260805020000: {UP: func(d migration.Datasource) error {
			return separateSystemAdministration(d, dialect)
		}},
		20260810010000: {UP: func(d migration.Datasource) error {
			return retireTheReviewPath(d, dialect)
		}},
		20260810020000: {UP: func(d migration.Datasource) error {
			return repairAccountsWithoutARole(d, dialect)
		}},
	}
}

// repairAccountsWithoutARole puts back a role for every account left without one,
// and makes sure there is one to put back.
//
// The migration before this one moves whoever was a manager to the employee role.
// It does that with a subselect on the role name, and a subselect that matches
// nothing yields NULL rather than nothing - so on an installation that had renamed
// or deleted the default employee role, every ex-manager's role_id became NULL.
//
// That is worse than it sounds. The users table is read into a struct whose role id
// is a plain uint, so a NULL in that column is not a user with no rights: it is a
// scan error. The affected person cannot sign in, and every listing of accounts
// fails for everybody, because one unreadable row fails the whole query. An
// administrator would see an empty user list and no reason for it.
//
// Written as a new migration rather than as a correction to the one that caused it.
// That one has shipped; an installation that already ran it has the NULL rows now,
// and editing it would repair nothing while quietly changing history for anyone who
// has not upgraded yet. This runs in the same start-up either way.
//
// What it cannot do is guess which role a renamed one was meant to be. An
// installation that renamed the default employee role gets the default back,
// alongside whatever they renamed it to, and their ex-managers land in it: two roles
// where there was one, which is a tidying job for an administrator rather than an
// account nobody can sign in to.
func repairAccountsWithoutARole(d migration.Datasource, dialect string) error {
	// A destination has to exist before anybody can be pointed at it. Inserted only
	// when absent, so the ordinary upgrade - where it is there already - changes
	// nothing.
	var employees int

	countRole := sqldb.Rebind(dialect, "SELECT COUNT(*) FROM roles WHERE name = ?")
	if err := d.SQL.QueryRow(countRole, model.RoleEmployee).Scan(&employees); err != nil {
		return fmt.Errorf("looking for the %q role: %w", model.RoleEmployee, err)
	}

	if employees == 0 {
		if err := seedRole(d, dialect, employeeRole()); err != nil {
			return err
		}
	}

	// Everybody who has no role, whatever left them that way.
	adopt := sqldb.Rebind(dialect, `UPDATE users SET role_id =
		(SELECT id FROM roles WHERE name = ?) WHERE role_id IS NULL`)

	if _, err := d.SQL.Exec(adopt, model.RoleEmployee); err != nil {
		return fmt.Errorf("giving roleless accounts the %q role: %w", model.RoleEmployee, err)
	}

	return nil
}

// employeeRole is the seeded employee role, as a fresh installation gets it.
func employeeRole() model.Role {
	for _, role := range model.DefaultRoles() {
		if role.Name == model.RoleEmployee {
			return role
		}
	}

	// Unreachable while RoleEmployee is one of the defaults, and a compile-time
	// constant is not enough to promise that at run time.
	return model.Role{Name: model.RoleEmployee, Description: "Keeps their own time"}
}

// retireTheReviewPath removes the entry status and the role that reviewed it.
//
// An entry travelled open -> submitted -> approved, and an approved one was locked
// against every later change. That needs somebody to do the approving, and there is
// nobody: everyone keeps their own hours and the built-in administrator runs the
// installation rather than reading other people's work. A review path with no
// reviewer is a step that only ever stands between a person and the time they
// recorded.
//
// So the column goes, and with it the manager role, the approve right, and the
// nightly sweep that moved stale entries along. Whoever was a manager becomes an
// ordinary account, which now carries everything about its own work - including its
// own projects.
func retireTheReviewPath(d migration.Datasource, dialect string) error {
	// The people first, while the role still exists to be moved away from. An
	// account left pointing at a role that has gone cannot sign in.
	moveUsers := sqldb.Rebind(dialect, `UPDATE users SET role_id =
		(SELECT id FROM roles WHERE name = ?)
		WHERE role_id IN (SELECT id FROM roles WHERE name = ?)`)

	if _, err := d.SQL.Exec(moveUsers, model.RoleEmployee, "manager"); err != nil {
		return fmt.Errorf("moving managers to %q: %w", model.RoleEmployee, err)
	}

	// Then the role, permissions first: role_permissions references roles(id), and
	// on the engines that enforce it the delete would be refused.
	for _, statement := range []string{
		`DELETE FROM role_permissions WHERE role_id IN (SELECT id FROM roles WHERE name = ?)`,
		`DELETE FROM roles WHERE name = ?`,
	} {
		if _, err := d.SQL.Exec(sqldb.Rebind(dialect, statement), "manager"); err != nil {
			return fmt.Errorf("removing the manager role: %w", err)
		}
	}

	// The approve right is gone from the application, so any role still holding it
	// holds a permission no line of code reads.
	if _, err := d.SQL.Exec(
		sqldb.Rebind(dialect, "DELETE FROM role_permissions WHERE permission = ?"),
		"timesheets:approve"); err != nil {
		return fmt.Errorf("revoking the approve permission: %w", err)
	}

	// What an ordinary account is now: its own time, and its own projects.
	grant := sqldb.Rebind(dialect, `INSERT INTO role_permissions (role_id, permission)
		SELECT id, ? FROM roles WHERE name = ?
		  AND NOT EXISTS (
			SELECT 1 FROM role_permissions rp
			WHERE rp.role_id = roles.id AND rp.permission = ?)`)

	for _, permission := range []string{
		model.PermProjectWrite, model.PermProjectArchive, model.PermProjectDelete,
		model.PermTimesheetTransfer,
	} {
		if _, err := d.SQL.Exec(grant, permission, model.RoleEmployee, permission); err != nil {
			return fmt.Errorf("granting %q to %q: %w", permission, model.RoleEmployee, err)
		}
	}

	// The description on screen, which would otherwise still promise submitting.
	describe := sqldb.Rebind(dialect, "UPDATE roles SET description = ? WHERE name = ?")

	for _, role := range model.DefaultRoles() {
		if _, err := d.SQL.Exec(describe, role.Description, role.Name); err != nil {
			return fmt.Errorf("describing %q: %w", role.Name, err)
		}
	}

	// And the column itself. Supported by all three engines in the versions this
	// application requires - SQLite has taken DROP COLUMN since 3.35, which predates
	// the driver in go.mod.
	if _, err := d.SQL.Exec("ALTER TABLE timesheets DROP COLUMN status"); err != nil {
		return fmt.Errorf("dropping the timesheet status: %w", err)
	}

	return nil
}

// separateSystemAdministration narrows the administrator role to the installation.
//
// The role was seeded holding every permission, which made the one account that
// exists on every installation also the account that could read, edit and approve
// everybody's recorded hours. Administering the installation and reading what
// people recorded in it are different jobs; see model.SystemAdminPermissions.
//
// Changing the seed only helps installations yet to be created, so the rights are
// revoked here for the ones that already ran it. The report right moves to the
// manager role in the same step, or it would be held by nobody at all.
//
// Only the permissions this migration is about are touched. An installation that
// has since added its own to either role keeps them: this is a correction of what
// was seeded, not a reset to what the seed says today.
func separateSystemAdministration(d migration.Datasource, dialect string) error {
	revoke := sqldb.Rebind(dialect, `DELETE FROM role_permissions
		WHERE permission = ?
		  AND role_id IN (SELECT id FROM roles WHERE name = ?)`)

	// What running the work is, as opposed to running the installation.
	for _, permission := range []string{
		model.PermTimesheetReadAll,
		model.PermTimesheetWriteAll,
		// Literals rather than the model's constants: a migration records what was
		// done at the time, and these two have since been removed from the model
		// along with the review path and the role. Referring to them would tie a
		// finished migration to a definition that is allowed to change.
		"timesheets:approve",
		model.PermTimesheetTransfer,
		model.PermReportRead,
		model.PermProjectWrite,
		model.PermProjectDelete,
		model.PermProjectArchive,
	} {
		if _, err := d.SQL.Exec(revoke, permission, model.RoleAdmin); err != nil {
			return fmt.Errorf("revoking %q from %q: %w", permission, model.RoleAdmin, err)
		}
	}

	// NOT EXISTS rather than a bare insert: an installation that already granted
	// these to the manager role would otherwise get a duplicate row, and the
	// pair is the primary key.
	grant := sqldb.Rebind(dialect, `INSERT INTO role_permissions (role_id, permission)
		SELECT id, ? FROM roles WHERE name = ?
		  AND NOT EXISTS (
			SELECT 1 FROM role_permissions rp
			WHERE rp.role_id = roles.id AND rp.permission = ?)`)

	// PermProjectDelete as well: it was the administrator's alone, and a project
	// with entries is refused, so what this deletes is an empty project created by
	// mistake rather than anybody's recorded time.
	for _, permission := range []string{model.PermReportRead, model.PermProjectDelete} {
		if _, err := d.SQL.Exec(grant, permission, "manager", permission); err != nil {
			return fmt.Errorf("granting %q to %q: %w", permission, "manager", err)
		}
	}

	// The descriptions are on screen in the role list, where "Full administrative
	// access" would now be describing something the role no longer is.
	describe := sqldb.Rebind(dialect, "UPDATE roles SET description = ? WHERE name = ?")

	for _, role := range model.DefaultRoles() {
		if role.Name != model.RoleAdmin && role.Name != "manager" {
			continue
		}

		if _, err := d.SQL.Exec(describe, role.Description, role.Name); err != nil {
			return fmt.Errorf("describing %q: %w", role.Name, err)
		}
	}

	return nil
}

// createRunningTimers holds the clock somebody has started and not yet stopped.
//
// A table of its own rather than columns on timesheets, because a running timer
// is not a time entry yet and cannot satisfy what a time entry has to: it has no
// duration until it stops, no status to be in, and no calendar day until the zone
// and the stop time are both known. Storing it as a timesheet would mean an entry
// of zero hours, which validation refuses and every total would have to learn to
// skip.
//
// The user is the primary key, so one person can have exactly one clock running.
// Two would be a question nobody has an answer for - which of them does the next
// stop belong to.
func createRunningTimers(d migration.Datasource, dialect string) error {
	return execAll(d, fmt.Sprintf(`CREATE TABLE running_timers (
		user_id %s PRIMARY KEY REFERENCES users(id),
		project_id %s REFERENCES projects(id),
		description TEXT,
		started_at %s NOT NULL
	)`, foreignKeyID(dialect), foreignKeyID(dialect), timestamp(dialect)))
}

// createPasskeys stores the WebAuthn credentials users register for signing in.
//
// Nothing here is worth stealing: a passkey's private half never leaves the
// device, so what is kept is the public key and the counters needed to verify a
// signature. That is also why there is no "revoke everything" concern - a
// credential is useless without the device that holds its other half.
//
// credential_id is unique across the installation because it is what a sign-in
// arrives with: the browser sends the credential, and the credential names its
// owner. That is what allows signing in without typing a username.
func createPasskeys(d migration.Datasource, dialect string) error {
	return execAll(d, fmt.Sprintf(`CREATE TABLE passkeys (
		%s,
		user_id %s NOT NULL REFERENCES users(id),
		name VARCHAR(120) NOT NULL,
		credential_id %s NOT NULL,
		public_key %s NOT NULL,
		attestation_type VARCHAR(32) NOT NULL DEFAULT '',
		transports VARCHAR(120) NOT NULL DEFAULT '',
		sign_count BIGINT NOT NULL DEFAULT 0,
		backup_eligible BOOLEAN NOT NULL DEFAULT FALSE,
		backed_up BOOLEAN NOT NULL DEFAULT FALSE,
		created_at %s NOT NULL,
		last_used_at %s
	)`, primaryKey(dialect), foreignKeyID(dialect),
		credentialID(dialect), blob(dialect),
		timestamp(dialect), timestamp(dialect)),
		"CREATE INDEX idx_passkeys_user ON passkeys (user_id)",
		"CREATE UNIQUE INDEX idx_passkeys_credential ON passkeys (credential_id)")
}

// credentialID is the type for the lookup key.
//
// MySQL cannot index a BLOB without a prefix length, so the identifier is
// stored as VARBINARY there - long enough for anything an authenticator
// produces, and indexable whole.
func credentialID(dialect string) string {
	if dialect == sqldb.DialectMySQL {
		return "VARBINARY(255)"
	}

	return blob(dialect)
}

// blob is the dialect's binary column type.
func blob(dialect string) string {
	switch dialect {
	case sqldb.DialectPostgres:
		return "BYTEA"
	case sqldb.DialectMySQL:
		return "BLOB"
	default:
		return "BLOB"
	}
}

// addUserTimezone lets an individual work in a zone other than the instance's.
//
// Empty means "follow the instance setting", which is what nearly everyone
// does; only someone working from another country needs their own. Storing the
// empty string rather than the resolved name matters, because it means changing
// the instance setting moves those people with it.
func addUserTimezone(d migration.Datasource, _ string) error {
	return execAll(d,
		"ALTER TABLE users ADD COLUMN timezone VARCHAR(64) NOT NULL DEFAULT ''",
	)
}

// addExternalIdentity gives directory-backed accounts a stable identifier.
//
// The synchronisation used to match on the mail address, which meant a renamed
// mailbox looked like one person leaving and another arriving - and the
// departure would have deleted their recorded hours.
//
// Existing rows keep an empty identifier and are matched by mail address until
// their next sign-in fills it in, so an upgrade loses nothing.
func addExternalIdentity(d migration.Datasource, dialect string) error {
	return execAll(d,
		"ALTER TABLE users ADD COLUMN external_id VARCHAR(128) NOT NULL DEFAULT ''",
		// Not UNIQUE: every local account carries the empty string, and most
		// engines would count those as duplicates.
		"CREATE INDEX idx_users_external_id ON users (external_id)",
	)
}

// addUserTourSeen remembers who has already been shown the guided tour.
//
// Existing accounts default to false and are therefore offered the tour once,
// which is the right outcome for an upgrade: they have not seen it either.
func addUserTourSeen(d migration.Datasource, _ string) error {
	return execAll(d,
		"ALTER TABLE users ADD COLUMN tour_seen BOOLEAN NOT NULL DEFAULT FALSE",
	)
}

// createAPITokens adds the personal tokens used for API access.
//
// A token stores only a hash, plus a short readable prefix so its owner can
// tell their tokens apart in a list without the secret being present.
func createAPITokens(d migration.Datasource, dialect string) error {
	return execAll(d, fmt.Sprintf(`CREATE TABLE api_tokens (
		%s,
		user_id %s NOT NULL REFERENCES users(id),
		name VARCHAR(120) NOT NULL,
		token_hash VARCHAR(64) NOT NULL UNIQUE,
		prefix VARCHAR(32) NOT NULL,
		created_at %s NOT NULL,
		expires_at %s,
		last_used_at %s
	)`, primaryKey(dialect), foreignKeyID(dialect),
		timestamp(dialect), timestamp(dialect), timestamp(dialect)),
		"CREATE INDEX idx_api_tokens_user ON api_tokens (user_id)")
}

// addPrivateProjects lets every user keep personal categories.
//
// A project with an owner is private to that user; NULL keeps the existing
// shared projects exactly as they were, so nothing has to be migrated.
func addPrivateProjects(d migration.Datasource, dialect string) error {
	err := execAll(d,
		fmt.Sprintf("ALTER TABLE projects ADD COLUMN owner_id %s REFERENCES users(id)",
			foreignKeyID(dialect)),
		"CREATE INDEX idx_projects_owner ON projects (owner_id)",
	)
	if err != nil {
		return err
	}

	// Grant the new permission to every existing role, so upgrading an
	// installation does not silently take the feature away from its users.
	return grantToAllRoles(d, dialect, model.PermProjectWriteOwn)
}

// grantToAllRoles adds a permission to every role that does not have it yet.
//
// The check is done per role in Go rather than with a single INSERT ... SELECT
// guarded by NOT EXISTS: referencing the target table inside an insert is not
// portable across the supported engines. Skipping existing rows matters
// because a freshly seeded admin role already holds every permission, and a
// duplicate insert would abort the migration.
func grantToAllRoles(d migration.Datasource, dialect, permission string) error {
	rows, err := d.SQL.Query("SELECT id FROM roles")
	if err != nil {
		return fmt.Errorf("reading roles: %w", err)
	}

	var roleIDs []int64

	for rows.Next() {
		var id int64
		if scanErr := rows.Scan(&id); scanErr != nil {
			_ = rows.Close()

			return fmt.Errorf("reading roles: %w", scanErr)
		}

		roleIDs = append(roleIDs, id)
	}

	err = rows.Err()
	_ = rows.Close()

	if err != nil {
		return fmt.Errorf("reading roles: %w", err)
	}

	countQuery := sqldb.Rebind(dialect,
		"SELECT COUNT(*) FROM role_permissions WHERE role_id = ? AND permission = ?")
	insertQuery := sqldb.Rebind(dialect,
		"INSERT INTO role_permissions (role_id, permission) VALUES (?, ?)")

	for _, roleID := range roleIDs {
		var existing int

		if err := d.SQL.QueryRow(countQuery, roleID, permission).Scan(&existing); err != nil {
			return fmt.Errorf("checking %q on role %d: %w", permission, roleID, err)
		}

		if existing > 0 {
			continue
		}

		if _, err := d.SQL.Exec(insertQuery, roleID, permission); err != nil {
			return fmt.Errorf("granting %q to role %d: %w", permission, roleID, err)
		}
	}

	return nil
}

// makeProjectOptional lets a time entry be booked without a project, so hours
// can be recorded first and categorised later (or not at all).
//
// SQLite cannot drop a NOT NULL constraint in place, so there the table is
// rebuilt; the other engines alter the column directly.
func makeProjectOptional(d migration.Datasource, dialect string) error {
	switch dialect {
	case sqldb.DialectPostgres:
		return execAll(d, "ALTER TABLE timesheets ALTER COLUMN project_id DROP NOT NULL")
	case sqldb.DialectMySQL:
		return execAll(d, fmt.Sprintf("ALTER TABLE timesheets MODIFY project_id %s NULL",
			foreignKeyID(dialect)))
	default: // sqlite
		return execAll(d,
			fmt.Sprintf(`CREATE TABLE timesheets_new (
				%s,
				user_id %s NOT NULL REFERENCES users(id),
				project_id %s REFERENCES projects(id),
				date %s NOT NULL,
				duration_hours %s NOT NULL,
				description TEXT,
				status VARCHAR(32) NOT NULL
			)`, primaryKey(dialect), foreignKeyID(dialect), foreignKeyID(dialect),
				timestamp(dialect), float(dialect)),

			"INSERT INTO timesheets_new (id, user_id, project_id, date, duration_hours, description, status) "+
				"SELECT id, user_id, project_id, date, duration_hours, description, status FROM timesheets",

			// Dropping the table takes its indexes with it, so they are
			// recreated against the replacement below.
			"DROP TABLE timesheets",
			"ALTER TABLE timesheets_new RENAME TO timesheets",
			"CREATE INDEX idx_timesheets_user_date ON timesheets (user_id, date)",
			"CREATE INDEX idx_timesheets_project ON timesheets (project_id)",
		)
	}
}

// createSettings adds the key/value table behind the administration screen:
// branding, and the LDAP connection.
//
// The database connection itself is deliberately *not* stored here - it would
// live in the very database it configures - and goes to a file instead.
func createSettings(d migration.Datasource, dialect string) error {
	return execAll(d, fmt.Sprintf(`CREATE TABLE settings (
		key_name VARCHAR(64) PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at %s NOT NULL
	)`, timestamp(dialect)))
}

// addSessionsAndPreferences introduces real sign-in sessions, per-user
// two-factor authentication and the user's interface language.
func addSessionsAndPreferences(d migration.Datasource, dialect string) error {
	err := execAll(d,
		// The token hash is the primary key: lookups happen on every request,
		// and there is nothing else worth indexing.
		fmt.Sprintf(`CREATE TABLE sessions (
			token_hash VARCHAR(64) PRIMARY KEY,
			user_id %s NOT NULL REFERENCES users(id),
			created_at %s NOT NULL,
			expires_at %s NOT NULL
		)`, foreignKeyID(dialect), timestamp(dialect), timestamp(dialect)),

		"CREATE INDEX idx_sessions_user ON sessions (user_id)",
		"CREATE INDEX idx_sessions_expiry ON sessions (expires_at)",

		// The secret stays empty until the user completes enrolment, so a
		// half-finished setup never locks anyone out.
		"ALTER TABLE users ADD COLUMN totp_secret VARCHAR(64) NOT NULL DEFAULT ''",
		fmt.Sprintf("ALTER TABLE users ADD COLUMN totp_enabled %s NOT NULL DEFAULT %s",
			boolean(dialect), falseLiteral(dialect)),
		"ALTER TABLE users ADD COLUMN language VARCHAR(8) NOT NULL DEFAULT ''",

		// Set by LDAP-provisioned accounts, which have no local password.
		fmt.Sprintf("ALTER TABLE users ADD COLUMN is_external %s NOT NULL DEFAULT %s",
			boolean(dialect), falseLiteral(dialect)),
	)
	if err != nil {
		return err
	}

	// Only the built-in administrator may read everyone's aggregate reports,
	// so the permission is withdrawn from every other role.
	return execAll(d, fmt.Sprintf(
		"DELETE FROM role_permissions WHERE permission = '%s' "+
			"AND role_id <> (SELECT id FROM roles WHERE name = '%s')",
		model.PermReportRead, model.RoleAdmin))
}

// addRoleBasedAccess introduces roles, credentials and per-user working times.
//
// Roles move from a free-text column on users into their own table so they can
// be administered at run time. Any role names already present are preserved,
// so an existing installation keeps its assignments.
func addRoleBasedAccess(d migration.Datasource, dialect string) error {
	err := execAll(d,
		fmt.Sprintf(`CREATE TABLE roles (
			%s,
			name VARCHAR(64) NOT NULL UNIQUE,
			description VARCHAR(255) NOT NULL DEFAULT '',
			is_system %s NOT NULL DEFAULT %s
		)`, primaryKey(dialect), boolean(dialect), falseLiteral(dialect)),

		fmt.Sprintf(`CREATE TABLE role_permissions (
			role_id %s NOT NULL REFERENCES roles(id),
			permission VARCHAR(64) NOT NULL,
			PRIMARY KEY (role_id, permission)
		)`, foreignKeyID(dialect)),

		// Nullable on purpose: the column is filled in below from the existing
		// role names, and only then would NOT NULL be satisfiable.
		fmt.Sprintf("ALTER TABLE users ADD COLUMN role_id %s", foreignKeyID(dialect)),
		"ALTER TABLE users ADD COLUMN password_hash VARCHAR(255) NOT NULL DEFAULT ''",
		fmt.Sprintf("ALTER TABLE users ADD COLUMN must_change_password %s NOT NULL DEFAULT %s",
			boolean(dialect), falseLiteral(dialect)),
		fmt.Sprintf("ALTER TABLE users ADD COLUMN is_system %s NOT NULL DEFAULT %s",
			boolean(dialect), falseLiteral(dialect)),
		fmt.Sprintf("ALTER TABLE users ADD COLUMN daily_target_hours %s NOT NULL DEFAULT 0", float(dialect)),
		fmt.Sprintf("ALTER TABLE users ADD COLUMN max_daily_hours %s NOT NULL DEFAULT 0", float(dialect)),
	)
	if err != nil {
		return err
	}

	if err := seedRoles(d, dialect); err != nil {
		return err
	}

	// Carry existing users over to the new roles, then retire the old column.
	// Anyone whose role name no longer exists lands on employee, the least
	// privileged role, rather than losing access entirely.
	return execAll(d,
		"UPDATE users SET role_id = (SELECT id FROM roles WHERE roles.name = users.role)",
		fmt.Sprintf("UPDATE users SET role_id = (SELECT id FROM roles WHERE name = '%s') WHERE role_id IS NULL",
			model.RoleEmployee),
		"ALTER TABLE users DROP COLUMN role",
	)
}

// seedRoles inserts the default roles and their permissions.
func seedRoles(d migration.Datasource, dialect string) error {
	for _, role := range model.DefaultRoles() {
		if err := seedRole(d, dialect, role); err != nil {
			return err
		}
	}

	return nil
}

// seedRole inserts one role and its permissions.
//
// Its own function because two places need it: first start, which writes every
// default role, and the repair above, which writes exactly one back.
func seedRole(d migration.Datasource, dialect string, role model.Role) error {
	insertRole := sqldb.Rebind(dialect,
		"INSERT INTO roles (name, description, is_system) VALUES (?, ?, ?)")

	if _, err := d.SQL.Exec(insertRole, role.Name, role.Description, role.IsSystem); err != nil {
		return fmt.Errorf("seeding role %q: %w", role.Name, err)
	}

	grant := sqldb.Rebind(dialect,
		"INSERT INTO role_permissions (role_id, permission) SELECT id, ? FROM roles WHERE name = ?")

	for _, permission := range role.Permissions {
		if _, err := d.SQL.Exec(grant, permission, role.Name); err != nil {
			return fmt.Errorf("granting %q to %q: %w", permission, role.Name, err)
		}
	}

	return nil
}

func execAll(d migration.Datasource, statements ...string) error {
	for _, stmt := range statements {
		if _, err := d.SQL.Exec(stmt); err != nil {
			return fmt.Errorf("migration statement failed (%s): %w", stmt, err)
		}
	}

	return nil
}

// primaryKey returns the dialect's auto-incrementing primary key declaration.
func primaryKey(dialect string) string {
	switch dialect {
	case sqldb.DialectPostgres:
		return "id SERIAL PRIMARY KEY"
	case sqldb.DialectMySQL:
		return "id INT AUTO_INCREMENT PRIMARY KEY"
	default: // sqlite
		return "id INTEGER PRIMARY KEY AUTOINCREMENT"
	}
}

// timestamp returns the dialect's type for a point in time.
func timestamp(dialect string) string {
	switch dialect {
	case sqldb.DialectPostgres:
		return "TIMESTAMPTZ"
	case sqldb.DialectMySQL:
		return "DATETIME"
	default: // sqlite
		return "TIMESTAMP"
	}
}

// foreignKeyID returns the column type for a foreign key, which must match the
// referenced primary key's type exactly on MySQL.
func foreignKeyID(dialect string) string {
	if dialect == sqldb.DialectMySQL {
		return "INT"
	}

	return "INTEGER"
}

// float returns the dialect's double-precision floating point type. MySQL
// rejects PostgreSQL's "DOUBLE PRECISION" spelling.
func float(dialect string) string {
	if dialect == sqldb.DialectMySQL {
		return "DOUBLE"
	}

	return "DOUBLE PRECISION"
}

// boolean returns the dialect's boolean type.
func boolean(dialect string) string {
	if dialect == sqldb.DialectSQLite {
		// SQLite has no real boolean type; the declaration is documentation
		// and the values are stored as 0/1.
		return "INTEGER"
	}

	return "BOOLEAN"
}

// falseLiteral returns how the dialect spells false in a DEFAULT clause.
func falseLiteral(dialect string) string {
	if dialect == sqldb.DialectSQLite {
		return "0"
	}

	return "FALSE"
}

func createUsers(dialect string) string {
	return fmt.Sprintf(`CREATE TABLE users (
		%s,
		name VARCHAR(255) NOT NULL,
		email VARCHAR(255) NOT NULL UNIQUE,
		role VARCHAR(32) NOT NULL
	)`, primaryKey(dialect))
}

func createProjects(dialect string) string {
	return fmt.Sprintf(`CREATE TABLE projects (
		%s,
		name VARCHAR(255) NOT NULL,
		description TEXT,
		start_date %s NOT NULL,
		end_date %s,
		status VARCHAR(32) NOT NULL
	)`, primaryKey(dialect), timestamp(dialect), timestamp(dialect))
}

func createTimesheets(dialect string) string {
	return fmt.Sprintf(`CREATE TABLE timesheets (
		%s,
		user_id %s NOT NULL REFERENCES users(id),
		project_id %s NOT NULL REFERENCES projects(id),
		date %s NOT NULL,
		duration_hours %s NOT NULL,
		description TEXT,
		status VARCHAR(32) NOT NULL
	)`, primaryKey(dialect), foreignKeyID(dialect), foreignKeyID(dialect), timestamp(dialect), float(dialect))
}
