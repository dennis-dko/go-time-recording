//go:build browser

package browser

import (
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/dennis-dko/go-time-recording/test/harness"
)

// A passkey sign-in while the installation is out of service is turned away as a
// password sign-in is.
//
// The password sign-in checks maintenance once the account is known, and ends the
// session it opened if the account may not be here: an unused session is still a
// session, and one handed out during maintenance lets its holder back in the
// moment maintenance ends, without signing in. The passkey sign-in opened its
// session and answered 200. The page then ended it itself, on the first refusal
// that came back - which is the screen reflecting a rule the server did not keep,
// and a client that is not this page keeps the session.
func TestAPasskeySignInDuringMaintenanceIsTurnedAway(t *testing.T) {
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

	p.run("open My account", chromedp.Click(`.tab[data-view="settings"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#passkey-card", chromedp.ByID))

	p.run("register a passkey",
		chromedp.SendKeys(`#form-passkey input[name="name"]`, "Greta phone", chromedp.ByQuery),
		p.click(`#form-passkey button[type="submit"]`))

	p.waitForText("#table-passkeys tbody", "Greta phone")

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn(harness.AdminEmail, adminPassword)
	p.waitGone("#login-screen")

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-maintenance", chromedp.ByID))

	p.run("go out of service",
		p.click(`#form-maintenance input[name="enabled"]`),
		p.click(`#form-maintenance button[type="submit"]`),
		chromedp.WaitVisible(".confirm-overlay", chromedp.ByQuery))

	p.run("confirm", p.click(".confirm-overlay button:not(.secondary)"))
	p.waitForText("#toast", "out of service")

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	// What the server answered the passkey's last step, rather than what the page
	// made of it: the page ends a session the maintenance refusal reaches it
	// through, and the rule this case is about is the server's.
	p.run("listen to the passkey's last step", chromedp.Evaluate(`(() => {
		const server = api;
		window.passkeyAnswer = null;
		api = (path, options) => {
			const answer = server(path, options);
			if (path === '/auth/passkey/login' && options?.method === 'PUT') {
				answer.then(() => { window.passkeyAnswer = 200; },
					(err) => { window.passkeyAnswer = err.status ?? -1; });
			}
			return answer;
		};
		return 1;
	})()`, nil))

	p.run("sign in with the passkey", p.click("#login-passkey"))

	var answered int

	p.run("wait for the answer", chromedp.Poll(`window.passkeyAnswer`, &answered))

	if answered == 200 {
		t.Error("a passkey sign-in during maintenance opened a session for somebody " +
			"a password sign-in would have turned away")
	}
}
