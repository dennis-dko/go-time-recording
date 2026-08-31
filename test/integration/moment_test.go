//go:build integration

package integration

import (
	"net/http"
	"testing"
	"time"
)

// A credential says when it was last used, to the minute.
//
// "Last used" is the signal somebody checks when they think a token has been
// taken: not whether it was used, which they know, but when. The wire type these
// responses used formats a day and throws the rest away, so a token used at three
// in the morning and one used at three in the afternoon read identically, and a
// token used twice today read as used once.
//
// The same wire type is right for the day a booking belongs to - "which day did
// you work" has no time of day in it - which is exactly why this went unnoticed:
// the field was correct for the thing it was written for and wrong for the thing
// it was reused for.
//
// Written as an assertion about the format rather than about a value, because a
// value cannot show what is missing: 2026-08-31 is a perfectly good date, and the
// only way to see the defect is to ask whether a time of day survives the trip.

// carriesATimeOfDay fails when the field is a bare date rather than a moment.
func carriesATimeOfDay(t *testing.T, what, value string) {
	t.Helper()

	if value == "" {
		t.Fatalf("%s is empty", what)
	}

	if _, err := time.Parse(time.DateOnly, value); err == nil {
		t.Errorf("%s is %q, a bare date: a credential used twice in one day reports "+
			"the same moment for both, and one used at three in the morning reads "+
			"like one used at three in the afternoon", what, value)

		return
	}

	if _, err := time.Parse(time.RFC3339, value); err != nil {
		t.Errorf("%s is %q, which is neither a date nor a timestamp", what, value)
	}
}

type tokenResponse struct {
	ID         uint    `json:"id"`
	Name       string  `json:"name"`
	Secret     string  `json:"secret"`
	CreatedAt  string  `json:"createdAt"`
	ExpiresAt  *string `json:"expiresAt"`
	LastUsedAt *string `json:"lastUsedAt"`
}

func TestATokenSaysWhenItWasMadeAndWhenItWasLastUsed(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")
	anna := a.signInAsUser(admin, "Anna", "anna@example.com")

	var created tokenResponse

	anna.must(anna.api(http.MethodPost, "/me/tokens",
		map[string]any{"name": "ci", "expiresInDays": 30}),
		http.StatusCreated, http.StatusOK).Data(t, &created)

	carriesATimeOfDay(t, "the token's createdAt", created.CreatedAt)

	if created.ExpiresAt == nil {
		t.Fatal("a token asked to expire in 30 days reports no expiry")
	}

	carriesATimeOfDay(t, "the token's expiresAt", *created.ExpiresAt)

	// Used once, so there is a last-used moment to report.
	req := rawRequest(t, http.MethodGet, a.BaseURL()+"/api/v1/timesheets", "")
	req.Header.Set("Authorization", "Bearer "+created.Secret)

	if resp := sendPlain(t, req); resp.Status != http.StatusOK {
		t.Fatalf("the token should authenticate, got %d: %s", resp.Status, resp.Body)
	}

	var listed listOf[tokenResponse]

	anna.must(anna.api(http.MethodGet, "/me/tokens", nil), http.StatusOK).Data(t, &listed)

	if len(listed.Items) != 1 {
		t.Fatalf("the account has %d tokens, want 1", len(listed.Items))
	}

	if listed.Items[0].LastUsedAt == nil {
		t.Fatal("the token was used and reports no last use")
	}

	carriesATimeOfDay(t, "the token's lastUsedAt", *listed.Items[0].LastUsedAt)
}
