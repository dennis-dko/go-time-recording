//go:build browser

package browser

import (
	"encoding/json"
	"testing"

	"github.com/chromedp/chromedp"
)

// A half-finished enrolment does not outlive the session that started it.
//
// The panel holds a shared secret and the QR code that encodes it, and the code
// says twice that neither may hang about. renderTOTPState clears them - "The code
// encodes the secret, so it must not survive the enrolment it belongs to -
// neither on screen nor in the markup" - and switchView calls it on the way out
// of Settings, because "an enrolment in progress does not survive leaving the
// screen ... neither has any business sitting on a screen somebody has walked
// away from".
//
// Signing out is the strongest form of leaving the screen, and it did not call
// it. handBackTheScreen is the function that enumerates what must not be waiting
// for whoever signs in next - the drafts, the forms, the loose fields, the
// caches, the tables, the lists, the appearance, the language, the banners, the
// walk through, the overlays, the stopwatch, the tally - and a shared secret is a
// better reason than any of them.
//
// The next sign-in clears it through loadMe, so what this is about is the window
// in between, where the page is the sign-in form and the secret is still in the
// document behind it.
func TestAnUnfinishedEnrolmentDoesNotSurviveTheSignOut(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.run("start enrolling", p.click(`.tab[data-view="settings"]`),
		chromedp.WaitVisible("#totp-card", chromedp.ByID),
		p.click("#totp-begin"),
		chromedp.WaitVisible("#totp-setup", chromedp.ByID))

	read := func(when string) (secret, uri, src string, shown bool) {
		t.Helper()

		var out string

		p.run("read the panel "+when, chromedp.Evaluate(`JSON.stringify({
			secret: document.querySelector('#totp-secret').textContent.trim(),
			uri: document.querySelector('#totp-uri').textContent.trim(),
			src: document.querySelector('#totp-qr').getAttribute('src') ?? '',
			shown: !document.querySelector('#totp-setup').hidden,
		})`, &out))

		var panel struct {
			Secret string `json:"secret"`
			URI    string `json:"uri"`
			Src    string `json:"src"`
			Shown  bool   `json:"shown"`
		}

		if err := json.Unmarshal([]byte(out), &panel); err != nil {
			t.Fatalf("reading the panel (%q): %v", out, err)
		}

		return panel.Secret, panel.URI, panel.Src, panel.Shown
	}

	secret, uri, src, shown := read("while enrolling")

	if secret == "" || !shown {
		t.Fatalf("the enrolment panel is not showing a secret (secret %q, shown %v), "+
			"so this case would pass whatever the sign-out does", secret, shown)
	}

	p.run("sign out", p.click("#logout"),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	after, afterURI, afterSrc, stillShown := read("after signing out")

	if after != "" {
		t.Errorf("the shared secret is still in the document after signing out: %q "+
			"(it was %q)", after, secret)
	}

	if afterURI != "" {
		t.Errorf("the enrolment URI is still in the document after signing out: %q "+
			"(it was %q)", afterURI, uri)
	}

	if afterSrc != "" {
		t.Errorf("the QR code is still in the document after signing out, and it "+
			"encodes the same secret (src was %d characters, is now %d)",
			len(src), len(afterSrc))
	}

	if stillShown {
		t.Error("the enrolment panel is still the visible half of the account card " +
			"behind the sign-in screen")
	}
}
