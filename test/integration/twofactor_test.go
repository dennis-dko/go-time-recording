//go:build integration

package integration

import (
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
)

// Two-factor enrolment hands over a shared secret, and how it is handed over
// decides whether people get through it.
//
// The key was text only: sixteen characters to read off one screen and type into a
// phone, which is where enrolment goes wrong. It now comes with a QR code as well,
// rendered here rather than in the browser - a picture whose only purpose is to be
// read by a machine is not something to hand-roll in JavaScript.
//
// The text stays. A machine with no camera has no other way in, and a code that
// will not scan needs a fallback rather than a dead end.

type enrolment struct {
	Secret string `json:"secret"`
	URI    string `json:"uri"`
	QR     string `json:"qr"`
}

func beginEnrolment(t *testing.T, c *client) enrolment {
	t.Helper()

	var out enrolment

	c.must(c.api(http.MethodPost, "/me/totp", nil),
		http.StatusCreated, http.StatusOK).Data(t, &out)

	return out
}

func TestEnrolmentOffersAQRCodeAndTheKey(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	setup := beginEnrolment(t, admin)

	if setup.Secret == "" {
		t.Error("the enrolment carries no secret to type")
	}

	if !strings.HasPrefix(setup.URI, "otpauth://totp/") {
		t.Errorf("the enrolment URI is %q, want an otpauth:// one", setup.URI)
	}

	const prefix = "data:image/svg+xml;base64,"

	if !strings.HasPrefix(setup.QR, prefix) {
		t.Fatalf("the enrolment carries no SVG QR code: %.60s", setup.QR)
	}

	svg, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(setup.QR, prefix))
	if err != nil {
		t.Fatalf("the QR payload is not base64: %v", err)
	}

	drawn := string(svg)

	if !strings.Contains(drawn, "<svg") || !strings.Contains(drawn, "viewBox=") {
		t.Errorf("the QR payload is not an SVG document: %.80s", drawn)
	}

	// The picture has to be of this enrolment. The URI is encoded rather than
	// written into the markup, so what is checked is that a code was drawn at all
	// and that it is large enough to hold a URI of this length - a code for an
	// empty string would still be a valid SVG.
	if !strings.Contains(drawn, "h1v1h-1z") {
		t.Error("the SVG contains no modules")
	}

	// 21 modules is the smallest code there is, and a URI this long needs more;
	// with the four-module margin either side the viewBox cannot be near that.
	if strings.Contains(drawn, `viewBox="0 0 29 29"`) {
		t.Error("the code is the smallest version there is, too small for this URI")
	}
}

// The secret is the whole of the second factor, so a fresh enrolment must not
// hand out the same one twice.
func TestEachEnrolmentGetsItsOwnSecret(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	first := beginEnrolment(t, admin)
	second := beginEnrolment(t, admin)

	if first.Secret == second.Secret {
		t.Error("a second enrolment handed out the same secret")
	}

	if first.QR == second.QR {
		t.Error("a second enrolment handed out the same QR code")
	}
}

// Enrolment is the caller's own business: the code encodes a secret that signs
// somebody in, so it must not be obtainable for anybody else, by anybody.
func TestEnrolmentIsOnlyEverForTheCaller(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	admin.must(admin.api(http.MethodPost, "/users", map[string]any{
		"name": "Petra", "email": "petra@example.com",
		"role": "employee", "password": "petra-password-1",
	}), http.StatusCreated, http.StatusOK)

	petra := a.newClient()
	petra.signIn("petra@example.com", "petra-password-1")

	// Both can enrol themselves, and there is no route that takes a user id -
	// which is the point: the endpoint is under /me and reads the caller.
	mine := beginEnrolment(t, admin)
	hers := beginEnrolment(t, petra)

	if mine.Secret == hers.Secret {
		t.Error("two accounts enrolled with the same secret")
	}

	if !strings.Contains(hers.URI, "petra%40example.com") &&
		!strings.Contains(hers.URI, "petra@example.com") {
		t.Errorf("the enrolment URI does not name the account it is for: %q", hers.URI)
	}
}
