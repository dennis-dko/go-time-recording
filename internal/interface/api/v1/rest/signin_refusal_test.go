package rest

import (
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// A refused password sign-in is logged when it is the installation's to settle,
// and not when it is somebody's typo - or every mistyped password would bury the
// line an administrator is looking for.
func TestOnlyARefusalThatIsNotATypoIsLogged(t *testing.T) {
	for _, refusal := range []struct {
		err   error
		typo  bool
		named string
	}{
		{apperror.Invalidf("invalid credentials").WithCode("invalidCredentials"), true, "a wrong password"},
		{apperror.Invalidf("the two-factor code is not valid").WithCode("twoFactorCodeInvalid"), true, "a wrong code"},
		{apperror.Invalidf("used").WithCode("twoFactorCodeUsed"), true, "a code used twice"},
		{apperror.Conflictf("another entry's").WithCode("accountOfAnotherEntry", "a@example.com"), false,
			"the account of another directory entry"},
		{apperror.Invalidf("no code at all"), false, "a refusal nobody named"},
	} {
		if got := mistypedCredentials(refusal.err); got != refusal.typo {
			t.Errorf("%s is read as a typo: %v, want %v", refusal.named, got, refusal.typo)
		}
	}
}
