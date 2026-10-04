//go:build integration

package integration

import (
	"database/sql"
	"slices"
	"testing"
	"time"

	"gofr.dev/pkg/gofr/migration"

	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/migrations"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/sqldb"
	"github.com/dennis-dko/go-time-recording/test/harness"
)

// theFirstRelease is the last migration of the first version: its tables and
// their indexes.
const theFirstRelease = int64(20260730120100)

// The whole chain runs over an installation that has been in use since the first
// version, on whichever dialect the suite is pointed at.
//
// Every other test of the chain starts from an empty database, and an empty table
// hides the one kind of mistake a migration can only make on somebody's data: a
// column added NOT NULL without a default is refused by PostgreSQL only once the
// table holds a row, and what a data migration assumes about the rows it meets is
// never tried by rows that are not there. So this takes a database to the first
// version, fills it the way that version's application would have - the role
// names it knew and one it did not, a shared project three people booked on and
// one nobody did, entries in every state the review path had - and runs the rest.
func TestTheWholeChainUpgradesAnInstallationInUseSinceTheFirstVersion(t *testing.T) {
	t.Parallel()

	dialect, db := harness.EmptyDatabase(t)
	all := migrations.All(dialect)

	run := func(after, through int64) {
		t.Helper()

		versions := make([]int64, 0, len(all))

		for version := range all {
			if version > after && version <= through {
				versions = append(versions, version)
			}
		}

		slices.Sort(versions)

		for _, version := range versions {
			if err := all[version].UP(migration.Datasource{SQL: db}); err != nil {
				t.Fatalf("migration %d on %s, over the first version's rows: %v", version, dialect, err)
			}
		}
	}

	run(0, theFirstRelease)

	exec := func(query string, args ...any) {
		t.Helper()

		if _, err := db.Exec(sqldb.Rebind(dialect, query), args...); err != nil {
			t.Fatalf("filling the first version's tables: %v", err)
		}
	}

	for _, person := range [][]string{
		{"Ada", "ada@example.com", "admin"},
		{"Ben", "ben@example.com", "employee"},
		{"Cleo", "cleo@example.com", "manager"},
		{"Dev", "dev@example.com", "auditor"},
	} {
		exec("INSERT INTO users (name, email, role) VALUES (?, ?, ?)", person[0], person[1], person[2])
	}

	day := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	exec("INSERT INTO projects (name, description, start_date, status) VALUES (?, ?, ?, ?)",
		"Shared", "booked on by three", day, "active")
	exec("INSERT INTO projects (name, description, start_date, status) VALUES (?, ?, ?, ?)",
		"Unbooked", "nobody's", day, "active")

	for _, entry := range []struct {
		email  string
		hours  float64
		status string
	}{
		{"ben@example.com", 2.5, "open"},
		{"ben@example.com", 1, "submitted"},
		{"cleo@example.com", 1.25, "approved"},
		{"dev@example.com", 3, "rejected"},
	} {
		exec(`INSERT INTO timesheets (user_id, project_id, date, duration_hours, description, status)
			VALUES ((SELECT id FROM users WHERE email = ?), (SELECT id FROM projects WHERE name = ?), ?, ?, ?, ?)`,
			entry.email, "Shared", day, entry.hours, "work", entry.status)
	}

	var highest int64
	for version := range all {
		highest = max(highest, version)
	}

	run(theFirstRelease, highest)

	count := func(query string) float64 {
		t.Helper()

		var n sql.NullFloat64
		if err := db.QueryRow(query).Scan(&n); err != nil {
			t.Fatalf("reading the upgraded database: %v", err)
		}

		return n.Float64
	}

	if n := count("SELECT COUNT(*) FROM users WHERE role_id IS NULL"); n != 0 {
		t.Errorf("%v account(s) came out of the upgrade without a role", n)
	}

	if n, hours := count("SELECT COUNT(*) FROM timesheets"),
		count("SELECT SUM(duration_hours) FROM timesheets"); n != 4 || hours != 7.75 {
		t.Errorf("the upgrade left %v entries and %v hours, want the 4 entries and 7.75 hours "+
			"recorded before it", n, hours)
	}

	if n := count("SELECT COUNT(*) FROM projects WHERE owner_id IS NULL"); n != 0 {
		t.Errorf("%v project(s) came out of the upgrade belonging to nobody", n)
	}

	// One copy of the shared project for each of the three who booked on it; the
	// one nobody booked on is gone.
	if n := count("SELECT COUNT(*) FROM projects"); n != 3 {
		t.Errorf("the upgrade left %v projects, want one for each of the three who booked", n)
	}

	if n := count(`SELECT COUNT(*) FROM timesheets t JOIN projects p ON p.id = t.project_id
		WHERE p.owner_id <> t.user_id`); n != 0 {
		t.Errorf("%v entries point at a project that belongs to somebody else", n)
	}
}
