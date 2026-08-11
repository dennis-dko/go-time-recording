package migrations_test

import (
	"database/sql"
	"path/filepath"
	"sort"
	"testing"

	"gofr.dev/pkg/gofr/migration"

	_ "modernc.org/sqlite"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/migrations"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/sqldb"
)

// Nobody comes out of an upgrade without a role.
//
// The migration that retires the review path moves whoever was a manager to the
// user role, by name, with a subselect. A subselect that matches nothing gives
// NULL rather than nothing, so on an installation that had renamed or deleted the
// default user role every ex-manager's role_id became NULL.
//
// That is not "a user with no rights". The users table is read into a struct whose
// role id is a plain uint, so a NULL there is a scan error: the person cannot sign
// in, and every listing of accounts fails for everybody, because one unreadable row
// fails the whole query. An administrator would see an empty user list and no reason
// for it.

// migrate runs the migrations after one version and up to another, in order.
//
// A lower bound as well as an upper one, because these tests take an installation
// to a point, change it the way an administrator might have, and then finish the
// upgrade - and running the early ones a second time fails on CREATE TABLE.
func migrate(t *testing.T, db *sql.DB, after, through int64) {
	t.Helper()

	all := migrations.All(sqldb.DialectSQLite)

	versions := make([]int64, 0, len(all))

	for version := range all {
		if version > after && version <= through {
			versions = append(versions, version)
		}
	}

	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })

	for _, version := range versions {
		if err := all[version].UP(migration.Datasource{SQL: db}); err != nil {
			t.Fatalf("migration %d: %v", version, err)
		}
	}
}

// latest is the highest version there is, so a test cannot silently stop short of a
// migration somebody adds later.
func latest(t *testing.T) int64 {
	t.Helper()

	var highest int64

	for version := range migrations.All(sqldb.DialectSQLite) {
		if version > highest {
			highest = version
		}
	}

	if highest == 0 {
		t.Fatal("there are no migrations")
	}

	return highest
}

func freshDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "upgrade.db"))
	if err != nil {
		t.Fatalf("opening the database: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	return db
}

// retirement is the migration that removes the review path, and the point an
// installation is taken to just before, so the state it had can be set up.
const retirement = int64(20260810010000)

func TestNobodyIsLeftWithoutARoleByTheUpgrade(t *testing.T) {
	t.Parallel()

	// Whatever an administrator did to the default user role before upgrading.
	for _, how := range []struct {
		name string
		do   func(t *testing.T, db *sql.DB)
	}{
		{
			name: "renamed",
			do: func(t *testing.T, db *sql.DB) {
				t.Helper()

				if _, err := db.Exec(`UPDATE roles SET name = 'Teamleiter' WHERE name = ?`,
					model.RoleUser); err != nil {
					t.Fatalf("renaming the user role: %v", err)
				}
			},
		},
		{
			name: "deleted",
			do: func(t *testing.T, db *sql.DB) {
				t.Helper()

				for _, statement := range []string{
					`DELETE FROM role_permissions WHERE role_id =
						(SELECT id FROM roles WHERE name = ?)`,
					`DELETE FROM roles WHERE name = ?`,
				} {
					if _, err := db.Exec(statement, model.RoleUser); err != nil {
						t.Fatalf("deleting the user role: %v", err)
					}
				}
			},
		},
		{
			name: "left alone",
			do:   func(t *testing.T, db *sql.DB) { t.Helper() },
		},
	} {
		t.Run(how.name, func(t *testing.T) {
			t.Parallel()

			db := freshDB(t)

			// Up to the point before the review path is retired, which is the state
			// an installation being upgraded is in.
			migrate(t, db, 0, retirement-1)

			// A manager, which is what that migration has to find somewhere to put.
			if _, err := db.Exec(`INSERT INTO roles (name, description, is_system)
				VALUES ('manager', 'Reviews time entries', 0)`); err != nil {
				t.Fatalf("creating the manager role: %v", err)
			}

			if _, err := db.Exec(`INSERT INTO users
				(name, email, role_id, password_hash, daily_target_hours, max_daily_hours)
				VALUES ('Bea', 'bea@example.test',
					(SELECT id FROM roles WHERE name = 'manager'), 'x', 8, 12)`); err != nil {
				t.Fatalf("creating the manager account: %v", err)
			}

			how.do(t, db)

			// And now the upgrade, all the way to the end.
			migrate(t, db, retirement-1, latest(t))

			// The fact that matters: not one account without a role. A NULL here is
			// not a user with reduced rights, it is a row nothing can read.
			var roleless int

			if err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE role_id IS NULL`).
				Scan(&roleless); err != nil {
				t.Fatalf("counting accounts without a role: %v", err)
			}

			if roleless != 0 {
				t.Errorf("%d account(s) have no role after the upgrade; each one is a scan "+
					"error that fails every listing of accounts, not merely an account "+
					"with nothing granted to it", roleless)
			}

			// And it is readable as the application reads it: a plain uint, which is
			// what a NULL breaks.
			var (
				name   string
				roleID uint
			)

			err := db.QueryRow(`SELECT name, role_id FROM users WHERE email = ?`,
				"bea@example.test").Scan(&name, &roleID)
			if err != nil {
				t.Fatalf("reading the ex-manager back the way the application does: %v", err)
			}

			if roleID == 0 {
				t.Error("the ex-manager's role id is zero, which points at no role")
			}
		})
	}
}

// The repair leaves an ordinary upgrade exactly as it was.
//
// It inserts the user role only when there is none, so the common case - where
// it is there already - must not end up with two of them or with anybody moved.
func TestTheRepairChangesNothingOnAnOrdinaryUpgrade(t *testing.T) {
	t.Parallel()

	db := freshDB(t)
	migrate(t, db, 0, latest(t))

	var users int

	if err := db.QueryRow(`SELECT COUNT(*) FROM roles WHERE name = ?`,
		model.RoleUser).Scan(&users); err != nil {
		t.Fatalf("counting the user role: %v", err)
	}

	if users != 1 {
		t.Errorf("there are %d roles named %q after a clean run, want 1",
			users, model.RoleUser)
	}
}
