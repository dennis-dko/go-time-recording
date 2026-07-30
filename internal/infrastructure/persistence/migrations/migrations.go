// Package migrations defines the database schema as GoFr migrations, so a
// fresh binary provisions its own database on first start and no external
// schema tooling is needed for deployment.
package migrations

import (
	"fmt"

	"gofr.dev/pkg/gofr/migration"

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
	}
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
