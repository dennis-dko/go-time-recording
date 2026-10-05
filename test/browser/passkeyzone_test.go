//go:build browser

package browser

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// A passkey sign-in describes the account in the installation's zone, as every
// other way of signing in does.
//
// The answer to a sign-in carries the account, and an account that keeps no zone
// of its own follows the installation's. The password and the ticket said so;
// the passkey described it in UTC, which is nobody's zone in particular. This
// page reads the account again at once and never showed it, but the answer said
// something untrue to whatever else reads it.
func TestAPasskeySignInDescribesTheAccountInTheInstallationsZone(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.withAuthenticator(t)
	p.readyAdmin()

	var status string

	p.run("put the installation in Berlin", chromedp.Evaluate(`(async () => {
		const csrf = document.cookie.split(';').map(c => c.trim())
			.find(c => c.startsWith('gtr_csrf='))?.slice('gtr_csrf='.length) ?? '';

		const r = await fetch('/api/v1/settings/timezone', {
			method: 'PUT',
			credentials: 'same-origin',
			headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
			body: JSON.stringify({ timezone: 'Europe/Berlin' }),
		});

		return String(r.status);
	})()`, &status, awaitPromise))

	if status != "200" && status != "201" {
		t.Fatalf("could not set the installation's zone: HTTP %s", status)
	}

	p.createOrdinaryAccount(t, "zora@example.com", "zora-password-1")

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn("zora@example.com", "zora-password-1")
	p.waitGone("#login-screen")
	p.settleWelcome()

	// A first sign-in may take this browser's zone for the account; it is put
	// back to following the installation, which is the case the answer is for.
	p.run("follow the installation's zone", chromedp.Evaluate(`(async () => {
		await api('/me/timezone', { method: 'PUT', body: JSON.stringify({ timezone: '' }) });
		return 1;
	})()`, nil, awaitPromise))

	p.run("open My account", chromedp.Click(`.tab[data-view="settings"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#passkey-card", chromedp.ByID))

	p.run("register a passkey",
		chromedp.SendKeys(`#form-passkey input[name="name"]`, "Zora phone", chromedp.ByQuery),
		p.click(`#form-passkey button[type="submit"]`))

	p.waitForText("#table-passkeys tbody", "Zora phone")

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	// What the server answered the passkey's last step, before the page reads the
	// account again and draws that instead.
	p.run("listen to the passkey's last step", chromedp.Evaluate(`(() => {
		const server = api;
		window.passkeyZone = null;
		api = (path, options) => {
			const answer = server(path, options);
			if (path === '/auth/passkey/login' && options?.method === 'PUT') {
				answer.then((body) => { window.passkeyZone = body?.user?.effectiveTimezone ?? '(none)'; },
					() => { window.passkeyZone = '(refused)'; });
			}
			return answer;
		};
		return 1;
	})()`, nil))

	p.run("sign in with the passkey", p.click("#login-passkey"))

	var zone string

	p.run("wait for the answer", chromedp.Poll(`window.passkeyZone`, &zone))

	if zone != "Europe/Berlin" {
		t.Errorf("the passkey sign-in described an account following the installation's zone in %q, "+
			"want Europe/Berlin", zone)
	}
}
