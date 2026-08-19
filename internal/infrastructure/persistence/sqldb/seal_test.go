package sqldb_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/sqldb"
	"github.com/dennis-dko/go-time-recording/internal/pkg/security"
)

func aSealer(t *testing.T) *security.Sealer {
	t.Helper()

	raw := make([]byte, security.KeyBytes)
	if _, err := rand.Read(raw); err != nil {
		t.Fatalf("no randomness: %v", err)
	}

	sealer, err := security.NewSealer(base64.StdEncoding.EncodeToString(raw))
	if err != nil {
		t.Fatalf("build a sealer: %v", err)
	}

	return sealer
}

// storedSecret reads the column rather than the value, which is the only way to
// see whether anything was actually encrypted: every read path decrypts.
func storedSecret(t *testing.T, db *sql.DB, id uint) string {
	t.Helper()

	var raw string
	if err := db.QueryRow("SELECT totp_secret FROM users WHERE id = ?", id).Scan(&raw); err != nil {
		t.Fatalf("read the column: %v", err)
	}

	return raw
}

// Turning encryption on has to reach what was already there.
//
// A second factor is enrolled once and then read for years. A key that applied
// only to what is written next would leave every enrolled account exactly as
// exposed as before, while the configuration said otherwise - so "encryption is
// on" would be true of almost nothing.
func TestSealStoredSecretsBringsWhatWasThereUnderTheKey(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	roles := sqldb.NewRoleRepository(db, sqldb.DialectSQLite)

	role, err := roles.GetByName(ctx, model.RoleUser)
	if err != nil {
		t.Fatalf("read seeded role: %v", err)
	}

	// Written the way an installation without a key wrote it: in the clear.
	plain := sqldb.NewUserRepository(db, sqldb.DialectSQLite)

	saved, err := plain.Save(ctx, &model.User{
		Name: "Enrolled", Email: "enrolled@example.com", RoleID: role.ID,
		TOTPSecret: "JBSWY3DPEHPK3PXP", TOTPEnabled: true,
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	if got := storedSecret(t, db, saved.ID); got != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("the fixture is not what this test is about: the column holds %q", got)
	}

	sealer := aSealer(t)

	moved, err := sqldb.SealStoredSecrets(ctx, db, sqldb.DialectSQLite, sealer)
	if err != nil {
		t.Fatalf("bring under the key: %v", err)
	}

	if moved != 1 {
		t.Errorf("moved %d values, want the one that was in the clear", moved)
	}

	stored := storedSecret(t, db, saved.ID)

	if stored == "JBSWY3DPEHPK3PXP" {
		t.Error("the second factor is still readable in the column")
	}

	if !security.IsSealed(stored) {
		t.Errorf("the column holds %q, which carries no marker", stored)
	}

	// And it still works: a repository with the key reads back what was enrolled.
	sealed := sqldb.NewUserRepository(db, sqldb.DialectSQLite).WithSecrets(sealer)

	back, err := sealed.GetByID(ctx, saved.ID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	if back.TOTPSecret != "JBSWY3DPEHPK3PXP" {
		t.Errorf("read back %q, want the enrolled secret", back.TOTPSecret)
	}
}

// The second start moves nothing.
//
// It reads the column rather than the value for exactly this reason: every read
// path decrypts, so a pass that asked the repository would be handed plaintext
// either way and would rewrite every row on every start - which is a new nonce
// for every secret, every boot, and a count that always lies.
func TestASecondPassMovesNothing(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	roles := sqldb.NewRoleRepository(db, sqldb.DialectSQLite)

	role, err := roles.GetByName(ctx, model.RoleUser)
	if err != nil {
		t.Fatalf("read seeded role: %v", err)
	}

	sealer := aSealer(t)

	users := sqldb.NewUserRepository(db, sqldb.DialectSQLite).WithSecrets(sealer)

	saved, err := users.Save(ctx, &model.User{
		Name: "Enrolled", Email: "enrolled@example.com", RoleID: role.ID,
		TOTPSecret: "JBSWY3DPEHPK3PXP", TOTPEnabled: true,
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	before := storedSecret(t, db, saved.ID)

	moved, err := sqldb.SealStoredSecrets(ctx, db, sqldb.DialectSQLite, sealer)
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}

	if moved != 0 {
		t.Errorf("a value already under the key was moved %d time(s)", moved)
	}

	if after := storedSecret(t, db, saved.ID); after != before {
		t.Error("an already-encrypted secret was rewritten, so every start would " +
			"churn every row")
	}
}

// An installation with no key is left exactly as it was.
func TestWithoutAKeyNothingIsTouched(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	none, err := security.NewSealer("")
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	moved, err := sqldb.SealStoredSecrets(ctx, db, sqldb.DialectSQLite, none)
	if err != nil {
		t.Fatalf("pass: %v", err)
	}

	if moved != 0 {
		t.Errorf("moved %d values on an installation with no key", moved)
	}
}
