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
	}
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
	insertRole := sqldb.Rebind(dialect,
		"INSERT INTO roles (name, description, is_system) VALUES (?, ?, ?)")
	grant := sqldb.Rebind(dialect,
		"INSERT INTO role_permissions (role_id, permission) SELECT id, ? FROM roles WHERE name = ?")

	for _, role := range model.DefaultRoles() {
		if _, err := d.SQL.Exec(insertRole, role.Name, role.Description, role.IsSystem); err != nil {
			return fmt.Errorf("seeding role %q: %w", role.Name, err)
		}

		for _, permission := range role.Permissions {
			if _, err := d.SQL.Exec(grant, permission, role.Name); err != nil {
				return fmt.Errorf("granting %q to %q: %w", permission, role.Name, err)
			}
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
