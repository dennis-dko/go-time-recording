//go:build browser

package browser

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/dennis-dko/go-time-recording/internal/pkg/security"
)

// This pins a decision rather than checking a rule, which is why it is worth
// having: with two-factor enabled and a passkey registered, signing in with the
// passkey does not ask for a code.
//
// That is deliberate. Registration and sign-in both demand
// protocol.VerificationRequired, so the device had to see a fingerprint or a PIN
// before it would sign - possession of the device plus verification of the
// person, which is already two factors. It is how Google, Microsoft and Apple
// treat passkeys too: they satisfy multi-factor rather than needing another one
// stacked on top.
//
// The consequence to be aware of: enabling two-factor does not force a second
// factor on someone who has a passkey, because their passkey is a way in that
// never asks. Anyone wanting two-factor as a policy needs more than this
// setting. The test exists so that if the behaviour is ever changed, it is
// changed on purpose.
func TestAPasskeySignsInWithoutATwoFactorCodeEvenWhenTOTPIsOn(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.withAuthenticator(t)

	p.readyAdmin()
	p.createOrdinaryAccount(t, "hanna@example.com", "hanna-password-1")

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn("hanna@example.com", "hanna-password-1")
	p.waitGone("#login-screen")
	p.settleWelcome()

	secret := p.enableTOTP(t)

	// Two-factor really is on: the password alone now stops at the code field.
	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn("hanna@example.com", "hanna-password-1")
	p.run("wait for the code field", chromedp.WaitVisible("#login-totp-field", chromedp.ByID))

	code, err := security.CurrentTOTPCode(secret)
	if err != nil {
		t.Fatalf("cannot compute a code: %v", err)
	}

	p.run("supply the code",
		chromedp.SendKeys(`#form-login input[name="totp"]`, code, chromedp.ByQuery),
		p.click(`#form-login button[type="submit"]`))
	p.waitGone("#login-screen")

	// Now register a passkey for the same account.
	p.run("open My account", chromedp.Click(`.tab[data-view="settings"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#passkey-card", chromedp.ByID))

	p.run("register a passkey",
		chromedp.SendKeys(`#form-passkey input[name="name"]`, "Hanna's laptop", chromedp.ByQuery),
		p.click(`#form-passkey button[type="submit"]`))

	p.waitForText("#table-passkeys tbody", "Hanna's laptop")

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	// And the passkey gets in with no code asked for.
	p.run("sign in with the passkey", p.click("#login-passkey"))
	p.waitGone("#login-screen")

	if p.visible("#login-totp-field") {
		t.Error("the passkey sign-in asked for a two-factor code, which it is not supposed to")
	}

	var who string

	p.run("read who is signed in",
		chromedp.Evaluate(`document.querySelector('#who')?.textContent ?? ''`, &who))

	if !strings.Contains(who, "hanna@example.com") && !strings.Contains(who, "Erika") {
		t.Errorf("expected to be signed in as Hanna, the header says %q", who)
	}
}

// enableTOTP turns two-factor on through the API and returns the secret, so the
// test can produce codes. Driving the enrolment form would be testing the form,
// which other cases already do.
func (p *page) enableTOTP(t *testing.T) string {
	t.Helper()

	// The session first, and waited for rather than assumed.
	//
	// The login screen going away means the sign-in answered, not that every
	// cookie it set is back in the browser and being attached to fetches. Firing
	// straight into an enrolment lost that race about once in a suite run, and
	// then reported it as "no secret was issued" - which describes the symptom and
	// points nowhere near the cause.
	p.waitSignedIn(t)

	var answer struct {
		Status int    `json:"status"`
		Secret string `json:"secret"`
		Body   string `json:"body"`
	}

	p.run("begin two-factor enrolment", chromedp.Evaluate(`
		(async () => {
			const csrf = document.cookie.split(';').map(c => c.trim())
				.find(c => c.startsWith('gtr_csrf='))?.slice('gtr_csrf='.length) ?? '';

			const r = await fetch('/api/v1/me/totp', {
				method: 'POST',
				credentials: 'same-origin',
				headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
			});

			const body = await r.text();

			let secret = '';
			try { secret = JSON.parse(body)?.data?.secret ?? ''; } catch {}

			return { status: r.status, secret, body: body.slice(0, 400) };
		})()`, &answer, awaitPromise))

	if answer.Secret == "" {
		t.Fatalf("no two-factor secret was issued: HTTP %d, %s\n\napplication log:\n%s",
			answer.Status, answer.Body, p.app.Log())
	}

	secret := answer.Secret

	code, err := security.CurrentTOTPCode(secret)
	if err != nil {
		t.Fatalf("cannot compute a code: %v", err)
	}

	var status string

	p.run("confirm two-factor enrolment", chromedp.Evaluate(`
		(async () => {
			const csrf = document.cookie.split(';').map(c => c.trim())
				.find(c => c.startsWith('gtr_csrf='))?.slice('gtr_csrf='.length) ?? '';

			const r = await fetch('/api/v1/me/totp', {
				method: 'PUT',
				credentials: 'same-origin',
				headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
				body: JSON.stringify({ code: '`+code+`' }),
			});

			return String(r.status);
		})()`, &status, awaitPromise))

	if status != "200" && status != "201" {
		t.Fatalf("could not enable two-factor: HTTP %s\n\napplication log:\n%s", status, p.app.Log())
	}

	return secret
}

// The enrolment QR code has to be on screen, and has to be the code the server
// drew rather than a broken image.
//
// A picture that fails to load still occupies its box, so "the element is there"
// proves nothing: this checks the browser decoded it, which for an SVG data URI
// means the markup parsed.
func TestTheTwoFactorQRCodeIsShown(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("start two-factor enrolment",
		chromedp.Click(`.tab[data-view="settings"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#totp-card", chromedp.ByID),
		p.click("#totp-begin"),
		chromedp.WaitVisible("#totp-qr", chromedp.ByQuery),
	)

	var loaded bool

	// naturalWidth is 0 for an image the browser could not decode, whatever the
	// element's own size is.
	p.run("check the code decoded", chromedp.Evaluate(`(() => {
		const img = document.querySelector('#totp-qr');
		return Boolean(img && img.complete && img.naturalWidth > 0);
	})()`, &loaded))

	if !loaded {
		t.Errorf("the QR code did not load\n\nsrc: %.60s\n\napplication log:\n%s",
			p.attr("#totp-qr", "src"), p.app.Log())
	}

	if src := p.attr("#totp-qr", "src"); !strings.HasPrefix(src, "data:image/svg+xml") {
		t.Errorf("the code is not an inline SVG: %.60s", src)
	}

	// The typed key is still reachable, folded away behind the picture.
	if p.text("#totp-secret") == "" {
		t.Error("the key to type is gone; a machine with no camera has no way in")
	}

	// Leaving the screen and coming back must not leave the secret on it: the code
	// encodes it, and the enrolment it belonged to is over.
	//
	// Away via Users, because this is the built-in administrator and the time screens
	// are not its. Any other screen would do; the point is only to leave this one.
	p.run("leave and come back",
		chromedp.Click(`.tab[data-view="users"]`, chromedp.ByQuery),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Click(`.tab[data-view="settings"]`, chromedp.ByQuery),
		chromedp.Sleep(400*time.Millisecond),
	)

	if p.visible("#totp-qr") {
		t.Error("the QR code survived the enrolment it belonged to")
	}

	if secret := p.text("#totp-secret"); secret != "" {
		t.Errorf("the secret is still on screen after leaving: %q", secret)
	}
}
