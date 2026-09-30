package service_test

import (
	"context"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/support/security"
)

// A second-factor code opens one sign-in, not every sign-in in its window.
//
// A code is accepted for its own thirty seconds and the one either side, so it
// stays good for about a minute and a half - and it stayed good for all of it,
// however often it was used. Somebody who saw it typed, together with the
// password, could sign in with it after its owner had. RFC 6238 says a verifier
// must not accept the same code a second time once it has accepted it once
// (section 5.2), and that is what this holds.
func TestASecondFactorCodeOpensOneSignInOnly(t *testing.T) {
	f := newFixture(t)
	email := "two.factor@example.com"
	id := localUserWithRole(t, f, email, "user")

	secret, err := security.NewTOTPSecret()
	if err != nil {
		t.Fatalf("secret: %v", err)
	}

	if err := f.userRepo.SetTOTP(context.Background(), id, secret, true); err != nil {
		t.Fatalf("enrol: %v", err)
	}

	code, err := security.CurrentTOTPCode(secret)
	if err != nil {
		t.Fatalf("code: %v", err)
	}

	if _, err := f.sessions.Login(context.Background(), email, "a-password-of-their-own", code); err != nil {
		t.Fatalf("the first sign-in with a fresh code was refused: %v", err)
	}

	if _, err := f.sessions.Login(context.Background(), email, "a-password-of-their-own", code); err == nil {
		t.Error("the same code opened a second sign-in")
	}
}
