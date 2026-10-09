//go:build browser

package browser

import (
	"bytes"
	"context"
	"encoding/base64"
	"strconv"
	"testing"
	"time"

	"github.com/chromedp/cdproto/webauthn"
	"github.com/chromedp/chromedp"
)

// A passkey the device made and this installation refused is dropped from the
// device, and one it may hold after all is not.
//
// The device creates the passkey before the server has seen it. A server that
// then refused it - the attempt had run out, the check failed - left it on the
// device, offered at every sign-in and refused at every one, with nothing on
// this side able to take it away. The Signal API lets the page say so. Only a
// refusal is said: a request the server failed on may have stored the passkey
// after all, and one it already holds is not unknown to it.
//
// Each answer is read before the next registration, because the device keeps one
// passkey per account: CTAP2 has a new discoverable credential for the same site
// and user replace the last, which is what the first version of this case
// measured instead.
func TestAPasskeyThisInstallationRefusedIsDroppedFromTheDevice(t *testing.T) {
	t.Parallel()

	p := open(t)
	device := p.withAuthenticator(t)

	p.readyAdmin()
	p.createOrdinaryAccount(t, "erika@example.com", "erika-password-1")

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn("erika@example.com", "erika-password-1")
	p.waitGone("#login-screen")
	p.settleWelcome()

	p.run("open My account", chromedp.Click(`.tab[data-view="settings"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#passkey-card", chromedp.ByID))

	// The registration's second half is answered here rather than by the server,
	// so each refusal is the one this case names; what the page does with it is
	// what is under test.
	p.run("answer the registrations", chromedp.Evaluate(`(() => {
		window.__made = [];
		window.__answers = [
			[409, { code: 'passkeyKnown', message: 'this passkey is already registered' }],
			[500, { code: 'internal', message: 'the request could not be completed' }],
			[400, { code: 'passkeyUnverified', message: 'the passkey could not be verified' }],
		];

		const real = window.fetch;
		window.fetch = async (input, init) => {
			const url = typeof input === 'string' ? input : input.url;

			if (url.includes('/me/passkeys/register') && init?.method === 'PUT') {
				window.__made.push(JSON.parse(init.body).credential.id);

				const [status, error] = window.__answers.shift();

				return new Response(JSON.stringify({ error }),
					{ status, headers: { 'Content-Type': 'application/json' } });
			}

			return real(input, init);
		};

		return 1;
	})()`, nil))

	holds := func(made string) bool {
		want, err := base64.RawURLEncoding.DecodeString(made)
		if err != nil {
			t.Fatalf("the page sent a credential id that is not base64url: %q", made)
		}

		found := false

		p.run("read what the device holds", chromedp.ActionFunc(func(ctx context.Context) error {
			credentials, err := webauthn.GetCredentials(device).Do(ctx)
			if err != nil {
				return err
			}

			for _, credential := range credentials {
				id, err := base64.StdEncoding.DecodeString(credential.CredentialID)
				if err != nil {
					id, err = base64.RawURLEncoding.DecodeString(credential.CredentialID)
				}

				if err == nil && bytes.Equal(id, want) {
					found = true
				}
			}

			return nil
		}))

		return found
	}

	register := func(attempt int) string {
		p.run("register a passkey",
			chromedp.SetValue(`#form-passkey input[name="name"]`, "Device "+strconv.Itoa(attempt), chromedp.ByQuery),
			p.click(`#form-passkey button[type="submit"]`))

		p.waitEvaluates("the registration is answered", `String(window.__made.length)`, strconv.Itoa(attempt))
		p.atRest()

		var made string

		p.run("read the passkey made", chromedp.Evaluate(`window.__made.at(-1)`, &made))

		return made
	}

	for attempt, why := range []string{"already known to the installation", "refused by a server that failed"} {
		made := register(attempt + 1)

		// A signal of the test's own, for a passkey nobody holds, settled before
		// the device is read: the browser hands signals on in order, so any the
		// page sent before it has been dealt with by the time it is.
		p.run("let any signal settle", chromedp.Evaluate(`PublicKeyCredential.signalUnknownCredential({
			rpId: location.hostname, credentialId: 'bm9ib2R5LWhvbGRzLXRoaXM' }).then(() => 1)`, nil, awaitPromise))

		if !holds(made) {
			t.Errorf("the passkey %s was dropped from the device", why)
		}
	}

	unverified := register(3)
	deadline := time.Now().Add(waitPatience)

	for holds(unverified) {
		if time.Now().After(deadline) {
			t.Fatal("the passkey the installation refused as unverifiable is still on the device")
		}

		time.Sleep(200 * time.Millisecond)
	}
}
