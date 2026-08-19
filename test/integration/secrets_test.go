//go:build integration

package integration

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/pkg/security"
)

// A copy of the database must not be a copy of everybody's second factor.
//
// Passwords have always been bcrypt, so a stolen dump gave none of those away.
// The second factor was in the clear beside them, which is the more useful half
// of the pair to steal: it is what someone with a leaked password still needs,
// and it is a seed rather than a hash - readable, replayable, and good until the
// person notices and re-enrols.
//
// The whole test is against a running instance rather than the sealer, because
// what is worth proving is not that AES works. It is that the value the
// application writes is not the value it was given, and that it can still read
// its own writing.
func TestASecondFactorIsNotReadableInTheDatabase(t *testing.T) {
	t.Parallel()

	raw := make([]byte, security.KeyBytes)
	if _, err := rand.Read(raw); err != nil {
		t.Fatalf("no randomness: %v", err)
	}

	a := start(t, "SECRET_KEY="+base64.StdEncoding.EncodeToString(raw))
	admin := a.signInAsAdmin("a-much-better-password")

	setup := beginEnrolment(t, admin)

	if setup.Secret == "" {
		t.Fatal("the enrolment carries no secret")
	}

	// Confirmed, so the secret is the account's and not a half-finished enrolment.
	code, err := security.CurrentTOTPCode(setup.Secret)
	if err != nil {
		t.Fatalf("generate a code: %v", err)
	}

	admin.must(admin.api(http.MethodPut, "/me/totp", map[string]any{"code": code}),
		http.StatusOK, http.StatusNoContent)

	// The column, not the value: every read path through the application
	// decrypts, so asking the API would prove nothing about what is on disk.
	db := a.DB(t)
	if db == nil {
		t.Skip("this instance exposes no database")
	}

	defer func() { _ = db.Close() }()

	var stored string

	if err := db.QueryRow(
		"SELECT totp_secret FROM users WHERE totp_secret <> ''").Scan(&stored); err != nil {
		t.Fatalf("read the column: %v", err)
	}

	if stored == setup.Secret {
		t.Fatal("the second factor is stored exactly as it was issued: a copy of " +
			"this database is a copy of everybody's authenticator")
	}

	if strings.Contains(stored, setup.Secret) {
		t.Errorf("the stored value contains the secret: %q", stored)
	}

	if !security.IsSealed(stored) {
		t.Errorf("the stored value carries no marker: %q", stored)
	}

	// And the application can still read its own writing, which is the half that
	// encryption breaks when it is done wrong: a second factor nobody can verify
	// locks every enrolled account out.
	again, err := security.CurrentTOTPCode(setup.Secret)
	if err != nil {
		t.Fatalf("generate a code: %v", err)
	}

	admin.must(admin.api(http.MethodDelete, "/me/totp?code="+again, nil),
		http.StatusOK, http.StatusNoContent)
}

// Without a key the installation keeps working and says what it is doing.
//
// The upgrade path has to be the quiet one: an existing installation that has
// never heard of SECRET_KEY starts, serves, and enrols second factors exactly as
// before. What it must not do is be silent about it.
func TestWithoutAKeyTheInstallationStillWorksAndSaysSo(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	setup := beginEnrolment(t, admin)

	code, err := security.CurrentTOTPCode(setup.Secret)
	if err != nil {
		t.Fatalf("generate a code: %v", err)
	}

	admin.must(admin.api(http.MethodPut, "/me/totp", map[string]any{"code": code}),
		http.StatusOK, http.StatusNoContent)

	if !strings.Contains(a.log(), "SECRET_KEY is not set") {
		t.Error("an installation storing second factors in the clear says nothing " +
			"about it in the log")
	}
}
