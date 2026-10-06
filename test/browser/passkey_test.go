//go:build browser

package browser

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/webauthn"
	"github.com/chromedp/chromedp"

	"github.com/dennis-dko/go-time-recording/test/harness"
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

// A passkey refused for the connection says the certificate is one of the reasons.
//
// passkeyProblem translates the DOMException name because the browser's own
// message is in its own language, and the comment above it says which cases the
// name tells apart: a dismissed prompt, a device that already holds one, "and a
// page served over plain HTTP, which cannot work at all". That last one is no
// longer the whole of it. Since Firefox 140 - the fix for CVE-2025-6433 - WebAuthn
// is refused whenever a certificate error override is in place, and Firefox
// reports that as SecurityError too.
//
// So the sentence named HTTPS and the address while the actual cause was a third
// thing, on a deployment shape this repository ships itself: deploy/ terminates
// TLS with a local CA, and a device that has not been given that CA gets exactly
// this. Measured on the running instance rather than reasoned about - six
// challenges issued, no credential ever returned, Firefox 155 on the phone.
func TestAPasskeyRefusedForTheConnectionNamesTheCertificate(t *testing.T) {
	t.Parallel()

	p := open(t)

	var said string

	p.run("ask what a SecurityError is reported as", chromedp.Evaluate(
		`passkeyProblem({ name: 'SecurityError' })`, &said))

	if !strings.Contains(strings.ToLower(said), "certificate") {
		t.Errorf("a passkey refused for the connection is reported as %q, which "+
			"names HTTPS and the address but not the certificate - and an untrusted "+
			"certificate is what produces this wherever TLS is terminated with a "+
			"local CA", said)
	}
}

// Chrome reports the same refusal under a different name, and it is not a
// dismissed prompt.
//
// The sibling of the case above, and the reason to look for one: the rule that a
// refused connection has to name the certificate was applied where Firefox puts
// it and nowhere else. Chromium maps a certificate error to NotAllowedError -
// read in authentication_credentials_container.cc rather than assumed - so the
// same phone on the same instance in the other browser is told "the prompt was
// dismissed, or it timed out. Nothing was changed", which is untrue about what
// happened and points at the person rather than at the connection.
//
// The message is what tells them apart, although the comment above passkeyProblem
// says the name is the part worth reading. That still holds for the wording, which
// is the browser's own; this reads it only as a signal, and the signal is a fixed
// string in Chromium's source rather than a sentence somebody phrased. If it is
// ever reworded the case falls back to what it says today, which is why matching
// loosely on "certificate" is safer here than matching Chromium's whole sentence.
//
// The second half of this case is the one that would otherwise go unnoticed: an
// ordinary dismissed prompt must keep saying so. A check that widens until it
// catches everything has only moved the untrue sentence somewhere else.
func TestACertificateRefusalIsNotReportedAsADismissedPrompt(t *testing.T) {
	t.Parallel()

	p := open(t)

	var refused, dismissed string

	p.run("ask what Chrome's certificate refusal is reported as", chromedp.Evaluate(
		`passkeyProblem({
			name: 'NotAllowedError',
			message: 'WebAuthn is not supported on sites with TLS certificate errors.',
		})`, &refused))

	p.run("ask what an ordinary dismissed prompt is reported as", chromedp.Evaluate(
		`passkeyProblem({
			name: 'NotAllowedError',
			message: 'The operation either timed out or was not allowed.',
		})`, &dismissed))

	if !strings.Contains(strings.ToLower(refused), "certificate") {
		t.Errorf("Chrome's certificate refusal is reported as %q, which names neither "+
			"the certificate nor anything the reader can act on", refused)
	}

	if !strings.Contains(strings.ToLower(dismissed), "dismissed") {
		t.Errorf("an ordinary dismissed prompt is reported as %q; telling the two "+
			"apart must not cost the case that is genuinely about the prompt", dismissed)
	}
}

// A passkey signs somebody in on its own, even with two-factor switched on.
//
// Written down because it is a decision and looked like an omission: the sign-in
// path for a passkey goes from FinishLogin straight to OpenSession and never
// consults the second factor, while the password path returns totpRequired and
// asks. Nothing in the tree said why, and somebody comparing the two paths would
// find the shorter one and reasonably read it as a hole.
//
// It is not one, because the second factor is what a passkey already is. Both
// ceremonies ask for protocol.VerificationRequired - at registration, where the
// comment gives the reason ("Without it, possession of an unlocked laptop would
// be the whole factor"), and again at BeginLogin, which is the half that matters
// here: it means the device must be held *and* unlocked before it will sign, so
// the possession and the knowledge arrive in one gesture. A code on top would be
// a third factor, and the weakest of the three.
//
// The control below is what makes this a case rather than an assertion about
// nothing: the password way into the same account must still ask for the code.
// Without it this would pass just as happily against an installation where the
// enrolment silently did nothing - which is the failure it exists to catch.
func TestAPasskeySignsInWithoutTheSecondFactor(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.withAuthenticator(t)

	p.readyAdmin()
	p.createOrdinaryAccount(t, "greta@example.com", "greta-password-1")

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn("greta@example.com", "greta-password-1")
	p.waitGone("#login-screen")
	p.settleWelcome()

	// Two-factor on, and a passkey beside it - the combination somebody who takes
	// security seriously actually ends up with.
	p.enableTOTP(t)

	p.run("open My account", chromedp.Click(`.tab[data-view="settings"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#passkey-card", chromedp.ByID))

	p.run("register a passkey",
		chromedp.SendKeys(`#form-passkey input[name="name"]`, "Greta phone", chromedp.ByQuery),
		p.click(`#form-passkey button[type="submit"]`),
	)

	p.waitForText("#table-passkeys tbody", "Greta phone")

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	// The control: the password still has to be joined by a code.
	p.signIn("greta@example.com", "greta-password-1")
	p.run("the password way in asks for the code",
		chromedp.WaitVisible("#login-totp-field", chromedp.ByID))

	// A fresh sign-in screen, so the passkey is used against a form that is not
	// half way through the other way in.
	p.run("back to a clean sign-in screen",
		chromedp.Navigate(p.app.BaseURL()),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	if !p.visible("#login-passkey") {
		t.Fatal("the passkey button is not offered, so the rest of this case would prove nothing")
	}

	p.run("sign in with the passkey alone", p.click("#login-passkey"))
	p.waitGone("#login-screen")

	if p.visible("#login-totp-field") {
		t.Error("the passkey sign-in asked for a code as well; the ceremony already " +
			"required the device to be unlocked, so this would be a third factor")
	}

	var who string

	p.run("read who is signed in",
		chromedp.Evaluate(`document.querySelector('#who')?.textContent ?? ''`, &who))

	if !strings.Contains(who, "greta@example.com") && !strings.Contains(who, "Erika") {
		t.Errorf("expected to be signed in as Greta, the header says %q", who)
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
		selector, want, p.text(selector)+p.recorded(), p.app.Log())
}

// recorded is whatever a case asked the page to write down, for the failure
// message. Empty when nothing did.
func (p *page) recorded() string {
	var states string

	p.run("read what the page recorded", chromedp.Evaluate(
		`JSON.stringify(window.__states ?? [])`, &states))

	if states == "[]" || states == "" {
		return ""
	}

	return " ; it passed through " + states
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

// markAsDirectoryAccount makes an account look like one the directory created,
// through the database because nothing else does it: a directory account cannot
// sign in with a local password, and standing a directory up for one flag would
// be testing the directory rather than the rule.
func (p *page) markAsDirectoryAccount(t *testing.T, email string) {
	t.Helper()

	db := p.app.DB(t)
	if db == nil {
		t.Fatal("this instance has no database")
	}

	query := "UPDATE users SET is_external = ?, external_id = ? WHERE email = ?"
	if strings.Contains(os.Getenv(harness.DSNEnv), "postgres") {
		query = "UPDATE users SET is_external = $1, external_id = $2 WHERE email = $3"
	}

	if _, err := db.Exec(query, true, "uid="+email, email); err != nil {
		t.Fatalf("marking %s as the directory's: %v", email, err)
	}
}

// An account the directory holds is not offered a passkey.
//
// The server refuses to register one - the directory decides whether such an
// account may still sign in, and a passkey never asks it - so offering the card
// would be offering a refusal. The session is opened while the account is still
// local and the account is handed to the directory underneath it, which is the
// one way to be signed in as a directory account without a directory.
func TestADirectoryAccountIsNotOfferedPasskeys(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.withAuthenticator(t)

	p.readyAdmin()
	p.createOrdinaryAccount(t, "erika@example.com", "erika-password-1")

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn("erika@example.com", "erika-password-1")
	p.waitGone("#login-screen")
	p.settleWelcome()

	p.markAsDirectoryAccount(t, "erika@example.com")
	p.reload()

	p.run("open My account", chromedp.Click(`.tab[data-view="settings"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#form-my-timezone", chromedp.ByID))

	if p.visible("#passkey-card") {
		t.Error("an account the directory holds is offered a passkey the server would refuse")
	}
}

// A passkey opens no session for an account the directory has come to hold.
//
// Registered while the account was local, and the account handed to the
// directory afterwards - the ordinary way an installation moves to one. Signing
// in with the passkey would then skip the directory, which is the one place that
// can end the account, for as long as the passkey exists.
func TestAPasskeyOpensNoSessionForADirectoryAccount(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.withAuthenticator(t)

	p.readyAdmin()
	p.createOrdinaryAccount(t, "erika@example.com", "erika-password-1")

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn("erika@example.com", "erika-password-1")
	p.waitGone("#login-screen")
	p.settleWelcome()

	p.run("open My account", chromedp.Click(`.tab[data-view="settings"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#passkey-card", chromedp.ByID))

	p.run("register a passkey",
		chromedp.SendKeys(`#form-passkey input[name="name"]`, "Test device", chromedp.ByQuery),
		p.click(`#form-passkey button[type="submit"]`),
	)

	p.waitForText("#table-passkeys tbody", "Test device")

	p.markAsDirectoryAccount(t, "erika@example.com")

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	// Either outcome ends the wait, so the unfixed application fails here at once
	// rather than on a timeout: signed in, or turned away with a sentence.
	p.run("sign in with the passkey", p.click("#login-passkey"),
		chromedp.Poll(`(() => {
			const screen = document.querySelector('#login-screen');
			const error = document.querySelector('#login-error');
			return (screen && screen.hidden) || (error && !error.hidden);
		})()`, nil, chromedp.WithPollingTimeout(20*time.Second)))

	if !p.visible("#login-screen") {
		t.Fatal("the passkey opened a session for an account the directory holds")
	}

	if !p.visible("#login-error") {
		t.Error("the refused sign-in says nothing")
	}
}

// A refusal the browser names in a way the screen does not know is still said in
// the reader's language.
//
// passkeyProblem translates the names it knows, because the browser's own message
// is in the browser's language - and for every other name it handed on that
// message alone, so an authenticator that failed for a reason of its own told a
// German reader "The operation failed for an unknown transient reason." and
// nothing else. The transport's own case was the same: the browser's exception on
// a translated screen, now a translated sentence with the browser's words kept as
// the detail.
func TestAnUnknownPasskeyRefusalIsSaidInTheReadersLanguage(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()
	p.chooseLanguage("de")

	var said string

	p.run("ask what an unknown refusal is reported as", chromedp.Evaluate(
		`passkeyProblem({
			name: 'UnknownError',
			message: 'The operation failed for an unknown transient reason.',
		})`, &said))

	if !strings.Contains(said, "Der Passkey wurde nicht akzeptiert") {
		t.Errorf("an unknown refusal is reported on a German screen as %q", said)
	}

	if !strings.Contains(said, "unknown transient reason") {
		t.Errorf("the browser's own words were dropped, which is all the detail there is: %q", said)
	}
}
