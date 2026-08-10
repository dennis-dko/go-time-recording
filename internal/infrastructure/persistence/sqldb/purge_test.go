package sqldb_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"gofr.dev/pkg/gofr/migration"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/migrations"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/sqldb"

	_ "modernc.org/sqlite"
)

// newTestDB opens a throwaway SQLite database and applies the real migrations,
// so the cascade is exercised against the schema that actually ships rather
// than a hand-written copy of it.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()

	path := filepath.Join(t.TempDir(), "purge-test.db")

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	all := migrations.All(sqldb.DialectSQLite)

	versions := make([]int64, 0, len(all))
	for version := range all {
		versions = append(versions, version)
	}

	// Migrations must run in their declared order.
	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })

	for _, version := range versions {
		if err := all[version].UP(migration.Datasource{SQL: db}); err != nil {
			t.Fatalf("migration %d: %v", version, err)
		}
	}

	return db
}

// PurgeUser must remove the account and everything that points at it, since a
// leftover row would reference a user that no longer exists.
func TestPurgeUserRemovesEveryReference(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	users := sqldb.NewUserRepository(db, sqldb.DialectSQLite)
	projects := sqldb.NewProjectRepository(db, sqldb.DialectSQLite)
	timesheets := sqldb.NewTimesheetRepository(db, sqldb.DialectSQLite)
	tokens := sqldb.NewAPITokenRepository(db, sqldb.DialectSQLite)
	sessions := sqldb.NewSessionRepository(db, sqldb.DialectSQLite)

	// The migrations seed the roles, so an existing one can be used.
	roles := sqldb.NewRoleRepository(db, sqldb.DialectSQLite)

	role, err := roles.GetByName(ctx, model.RoleEmployee)
	if err != nil {
		t.Fatalf("read seeded role: %v", err)
	}

	leaving, err := users.Save(ctx, &model.User{
		Name: "Leaving", Email: "leaving@example.com", RoleID: role.ID, IsExternal: true,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	staying, err := users.Save(ctx, &model.User{
		Name: "Staying", Email: "staying@example.com", RoleID: role.ID,
	})
	if err != nil {
		t.Fatalf("create second user: %v", err)
	}

	// A shared project, which must survive: it belongs to the installation.
	shared, err := projects.Save(ctx, &model.Project{
		Name: "Shared", StartDate: time.Now(), Status: model.ProjectStatusActive,
	})
	if err != nil {
		t.Fatalf("create shared project: %v", err)
	}

	// A private project of the departing user, which must go with them.
	private, err := projects.Save(ctx, &model.Project{
		Name: "Private", StartDate: time.Now(), Status: model.ProjectStatusActive,
		OwnerID: &leaving.ID,
	})
	if err != nil {
		t.Fatalf("create private project: %v", err)
	}

	for _, owner := range []*model.User{leaving, staying} {
		if _, err := timesheets.Save(ctx, &model.Timesheet{
			UserID: owner.ID, ProjectID: &shared.ID, Date: time.Now(),
			DurationHours: 4,
		}); err != nil {
			t.Fatalf("book for %s: %v", owner.Email, err)
		}
	}

	if _, err := tokens.Save(ctx, &model.APIToken{
		UserID: leaving.ID, Name: "ci", TokenHash: "hash", Prefix: "gtr_x", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("create token: %v", err)
	}

	err = sessions.Save(ctx, &model.Session{
		TokenHash: "session-hash", UserID: leaving.ID,
		CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// A passkey, which the purge used to forget. Its table has a foreign key to
	// users like the rest, so forgetting it does not fail quietly: on PostgreSQL
	// and MySQL the final delete is refused and the account can never be removed,
	// while on SQLite the credential outlives the person it belonged to.
	passkeys := sqldb.NewPasskeyRepository(db, sqldb.DialectSQLite)

	if _, err := passkeys.Save(ctx, &model.Passkey{
		UserID: leaving.ID, Name: "work laptop",
		CredentialID: []byte("credential-id"), PublicKey: []byte("public-key"),
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("create passkey: %v", err)
	}

	if err := users.PurgeUser(ctx, leaving.ID); err != nil {
		t.Fatalf("purge: %v", err)
	}

	// Nothing belonging to the departed account may remain.
	for _, check := range []struct {
		what  string
		query string
	}{
		{"users", "SELECT COUNT(*) FROM users WHERE id = ?"},
		{"timesheets", "SELECT COUNT(*) FROM timesheets WHERE user_id = ?"},
		{"private projects", "SELECT COUNT(*) FROM projects WHERE owner_id = ?"},
		{"api tokens", "SELECT COUNT(*) FROM api_tokens WHERE user_id = ?"},
		{"sessions", "SELECT COUNT(*) FROM sessions WHERE user_id = ?"},
		{"passkeys", "SELECT COUNT(*) FROM passkeys WHERE user_id = ?"},
	} {
		var count int
		if err := db.QueryRow(check.query, leaving.ID).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", check.what, err)
		}

		if count != 0 {
			t.Errorf("%d %s remained after the purge", count, check.what)
		}
	}

	// The other user, their hours and the shared project must be untouched.
	if _, err := users.GetByID(ctx, staying.ID); err != nil {
		t.Errorf("the remaining user must survive: %v", err)
	}

	if _, err := projects.GetByID(ctx, shared.ID); err != nil {
		t.Errorf("the shared project must survive: %v", err)
	}

	if _, err := projects.GetByID(ctx, private.ID); err == nil {
		t.Error("the private project should have been removed with its owner")
	}

	remaining, err := timesheets.GetAll(ctx)
	if err != nil {
		t.Fatalf("list timesheets: %v", err)
	}

	if len(remaining) != 1 || remaining[0].UserID != staying.ID {
		t.Errorf("expected only the remaining user's entry, got %d entries", len(remaining))
	}
}

// Purging twice must not fail differently the second time: the sync repeats a
// failed run, so the operation has to be safe to re-enter.
func TestPurgeUserIsSafeToRepeat(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	users := sqldb.NewUserRepository(db, sqldb.DialectSQLite)
	roles := sqldb.NewRoleRepository(db, sqldb.DialectSQLite)

	role, err := roles.GetByName(ctx, model.RoleEmployee)
	if err != nil {
		t.Fatalf("read seeded role: %v", err)
	}

	user, err := users.Save(ctx, &model.User{
		Name: "Gone", Email: "gone@example.com", RoleID: role.ID, IsExternal: true,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if err := users.PurgeUser(ctx, user.ID); err != nil {
		t.Fatalf("first purge: %v", err)
	}

	// The second run finds nothing left and reports that, rather than
	// corrupting anything.
	if err := users.PurgeUser(ctx, user.ID); err == nil {
		t.Error("purging an already-removed account should report it as missing")
	}
}
