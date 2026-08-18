//go:build browser

package browser

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/webauthn"
	"github.com/chromedp/chromedp"
)

// Passkeys are the one feature that cannot be tested by calling the API: the
// signature comes from a device, and the device is the part being trusted.
//
// Chrome can supply one. Its virtual authenticator behaves like a real
// platform authenticator - it generates a key pair, signs challenges, and
// refuses what a real one would refuse - without a fingerprint reader being
// present. So the whole ceremony runs for real, end to end, in CI.

// withAuthenticator attaches a virtual authenticator to the browser and
// returns its id.
func (p *page) withAuthenticator(t *testing.T) webauthn.AuthenticatorID {
	t.Helper()

	var id webauthn.AuthenticatorID

	p.run("attach a virtual authenticator", chromedp.ActionFunc(func(ctx context.Context) error {
		if err := webauthn.Enable().Do(ctx); err != nil {
			return err
		}

		created, err := webauthn.AddVirtualAuthenticator(&webauthn.VirtualAuthenticatorOptions{
			Protocol:  webauthn.AuthenticatorProtocolCtap2,
			Transport: webauthn.AuthenticatorTransportInternal,
			// The registration asks for a resident key and user verification;
			// an authenticator without both would refuse, and the failure would
			// look like a bug in the application.
			HasResidentKey:              true,
			HasUserVerification:         true,
			IsUserVerified:              true,
			AutomaticPresenceSimulation: true,
		}).Do(ctx)
		if err != nil {
			return err
		}

		id = created

		return nil
	}))

	t.Cleanup(func() {
		_ = chromedp.Run(p.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
			return webauthn.RemoveVirtualAuthenticator(id).Do(ctx)
		}))
	})

	return id
}

// A registered passkey signs in without a password being typed at all.
func TestRegisteringAPasskeyAndSigningInWithIt(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.withAuthenticator(t)

	// The built-in administrator keeps its password, so the passkey belongs to
	// an ordinary account - which is also the realistic case.
	p.readyAdmin()
	p.createOrdinaryAccount(t, "erika@example.com", "erika-password-1")

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn("erika@example.com", "erika-password-1")
	p.waitGone("#login-screen")
	p.settleWelcome()

	p.run("open My account", chromedp.Click(`.tab[data-view="settings"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#passkey-card", chromedp.ByID))

	// Registering is one form and one prompt, which the virtual authenticator
	// answers.
	p.run("register a passkey",
		chromedp.SendKeys(`#form-passkey input[name="name"]`, "Test device", chromedp.ByQuery),
		p.click(`#form-passkey button[type="submit"]`),
	)

	p.waitForText("#table-passkeys tbody", "Test device")

	// Now the part that matters: signing out and back in with no password.
	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	if !p.visible("#login-passkey") {
		t.Fatal("the passkey button should be offered on a secure context")
	}

	p.run("sign in with the passkey", p.click("#login-passkey"))
	p.waitGone("#login-screen")

	// Signed in as the right person, without a password having been typed.
	var who string

	p.run("read who is signed in",
		chromedp.Evaluate(`document.querySelector('#who')?.textContent ?? ''`, &who))

	if !strings.Contains(who, "erika@example.com") && !strings.Contains(who, "Erika") {
		t.Errorf("expected to be signed in as Erika, the header says %q", who)
	}
}

// Removing a passkey has to take effect, or revoking a lost device would be
// theatre.
func TestRemovingAPasskey(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.withAuthenticator(t)

	p.readyAdmin()
	p.createOrdinaryAccount(t, "frank@example.com", "frank-password-1")

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn("frank@example.com", "frank-password-1")
	p.waitGone("#login-screen")
	p.settleWelcome()

	p.run("register", chromedp.Click(`.tab[data-view="settings"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#passkey-card", chromedp.ByID),
		chromedp.SendKeys(`#form-passkey input[name="name"]`, "Doomed device", chromedp.ByQuery),
		p.click(`#form-passkey button[type="submit"]`),
	)

	p.waitForText("#table-passkeys tbody", "Doomed device")

	// Removing asks first now, like every other deletion here: a passkey is a way
	// into the account, and one click was all it took.
	p.run("remove it", p.click(`#table-passkeys tbody button.danger`),
		chromedp.WaitVisible(".confirm-overlay", chromedp.ByQuery))

	p.run("confirm", p.click(`.confirm-actions button.danger`))

	deadline := time.Now().Add(waitPatience)
	for time.Now().Before(deadline) {
		if !strings.Contains(p.text("#table-passkeys tbody"), "Doomed device") {
			return
		}

		time.Sleep(200 * time.Millisecond)
	}

	t.Fatalf("the passkey is still listed after removing it:\n%s", p.text("#table-passkeys tbody"))
}

// The built-in administrator is the way back into an installation. A way back
// in that depends on a particular device still existing is not one, so it is
// never offered the choice.
func TestTheBuiltInAdministratorIsNotOfferedPasskeys(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.withAuthenticator(t)

	p.readyAdmin()

	// Waits for the password form rather than the working times, which this
	// account no longer has: a daily target and a ceiling are figures nothing
	// would measure against for somebody who records no time, so that card is
	// gated on settings:write:own and the built-in administrator does not hold
	// it. Waiting for it here was waiting for something that never arrives.
	p.run("open My account", chromedp.Click(`.tab[data-view="settings"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#form-password", chromedp.ByID))

	if p.visible("#passkey-card") {
		t.Error("the built-in administrator must not be offered a passkey")
	}

	// And the reason this test now waits on something else, asserted rather than
	// left as a comment - otherwise the next person to see the working times
	// missing from this screen has no way to tell intent from regression.
	if p.visible("#form-working-times") {
		t.Error("the built-in administrator is offered working times, which it has no use for")
	}
}

// ------------------------------------------------------------------ helpers

// createOrdinaryAccount adds an ordinary account through the API, since the point of
// these tests is the passkey ceremony and not the staff form.
func (p *page) createOrdinaryAccount(t *testing.T, email, password string) {
	p.createAccount(t, email, password, "user")
}

// createAccount is the same for an account that holds a different role, which is
// how a screen that differs between two kinds of administrator can be looked at.
func (p *page) createAccount(t *testing.T, email, password, role string) {
	t.Helper()

	var status string

	p.run("create "+email, chromedp.Evaluate(`
		(async () => {
			const csrf = document.cookie.split(';').map(c => c.trim())
				.find(c => c.startsWith('gtr_csrf='))?.slice('gtr_csrf='.length) ?? '';

			const r = await fetch('/api/v1/users', {
				method: 'POST',
				credentials: 'same-origin',
				headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
				body: JSON.stringify({
					name: `+"`"+`Erika`+"`"+`, email: '`+email+`',
					role: '`+role+`', password: '`+password+`',
				}),
			});

			return String(r.status);
		})()`, &status, awaitPromise))

	if status != "200" && status != "201" {
		t.Fatalf("could not create %s: HTTP %s\n\napplication log:\n%s", email, status, p.app.Log())
	}
}

// waitForText polls until a selector contains the text.
func (p *page) waitForText(selector, want string) {
	p.t.Helper()

	deadline := time.Now().Add(waitPatience)

	for time.Now().Before(deadline) {
		if strings.Contains(p.text(selector), want) {
			return
		}

		time.Sleep(250 * time.Millisecond)
	}

	p.t.Fatalf("%s never contained %q; it says:\n%s\n\napplication log:\n%s",
		selector, want, p.text(selector), p.app.Log())
}

// waitForTextWithin is waitForText with its own patience, for the few things
// whose speed is somebody else's business - a connection to a port nobody
// answers on takes as long as the network stack takes.
func (p *page) waitForTextWithin(selector, want string, within time.Duration) {
	p.t.Helper()

	deadline := time.Now().Add(within)

	for time.Now().Before(deadline) {
		if strings.Contains(p.text(selector), want) {
			return
		}

		time.Sleep(250 * time.Millisecond)
	}

	p.t.Fatalf("%s never contained %q; it says:\n%s\n\napplication log:\n%s",
		selector, want, p.text(selector), p.app.Log())
}
