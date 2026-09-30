package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/support/security"
)

// The installer's token opens the built-in administrator only while the
// documented password would open it too, and alone.
//
// That equivalence is the whole of the claim's safety, and it was checked by a
// stand-in: whether the account still has to choose a password. An
// administrator resetting the built-in account's password sets that flag again,
// and so does nothing else about the claim - so in the process that served the
// installer, whoever held its token, which the log shows, could open the account
// again: without the password that had just been set, and without the second
// factor its owner had enrolled, which a sign-in with the documented password
// would still have asked for.
func TestTheInstallersTokenOpensOnlyWhatTheDocumentedPasswordWould(t *testing.T) {
	newSessions := func(t *testing.T) (*fixture, *service.SessionService, uint) {
		t.Helper()

		f := newFixture(t)

		if _, err := f.auth.EnsureSystemUser(context.Background()); err != nil {
			t.Fatalf("seed the built-in administrator: %v", err)
		}

		admin, err := f.userRepo.GetByEmail(context.Background(), service.SystemUserEmail)
		if err != nil {
			t.Fatalf("read the built-in administrator: %v", err)
		}

		return f, service.NewSessionService(f.userRepo, f.roleRepo, newStubSessions(),
			f.auth, time.Hour), admin.ID
	}

	t.Run("a fresh installation is handed over", func(t *testing.T) {
		_, sessions, _ := newSessions(t)

		if _, err := sessions.OpenFirstSession(context.Background()); err != nil {
			t.Fatalf("the claim on a fresh installation was refused: %v", err)
		}
	})

	t.Run("not after another administrator reset the password", func(t *testing.T) {
		f, sessions, admin := newSessions(t)

		if err := f.auth.SetPassword(context.Background(), admin, "a-reset-password-1"); err != nil {
			t.Fatalf("reset: %v", err)
		}

		if _, err := sessions.OpenFirstSession(context.Background()); err == nil {
			t.Error("the installer's token opened the built-in account after its password " +
				"had been reset, which the documented password no longer does")
		}
	})

	t.Run("not while a second factor stands in the way", func(t *testing.T) {
		f, sessions, admin := newSessions(t)

		secret, err := security.NewTOTPSecret()
		if err != nil {
			t.Fatalf("secret: %v", err)
		}

		if err := f.userRepo.SetTOTP(context.Background(), admin, secret, true); err != nil {
			t.Fatalf("enrol: %v", err)
		}

		if _, err := sessions.OpenFirstSession(context.Background()); err == nil {
			t.Error("the installer's token opened an account whose second factor a sign-in " +
				"with the documented password would still have asked for")
		}
	})
}
