//go:build browser

package browser

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/chromedp"

	"github.com/dennis-dko/go-time-recording/internal/support/security"
	"github.com/dennis-dko/go-time-recording/test/kerberostest"
	"github.com/dennis-dko/go-time-recording/test/tempdir"
)

// Signing in with the browser's Kerberos ticket, as the sign-in screen does it.
//
// Headless Chrome holds no ticket and trusts no address with one, so the ticket
// is added to the request on its way out through the DevTools protocol - the one
// thing a browser signed in to a domain does that this one cannot. The server
// checks it exactly as it checks a real one; the challenge round before it is
// the browser's business, and what the page does with the answer is this file's.
//
// Three of the four need the seeded directory, because a ticket's owner is looked
// up there, and skip without GTR_TEST_LDAP. CI runs them in the job that starts
// one, and fails that job when they skip.

// kerberosSecret derives the test keytab's key, and seals every ticket here.
const kerberosSecret = "the-service-secret"

// ticketsReady starts an installation that takes tickets and knows the seeded
// directory, and hands back its sign-in screen as a new tab would find it - not
// yet reloaded, since the page learns at start whether a ticket is on offer.
func ticketsReady(t *testing.T) *page {
	t.Helper()

	address := os.Getenv("GTR_TEST_LDAP")
	if address == "" {
		t.Skip("GTR_TEST_LDAP is not set; start test/docker-compose.yml's ldap profile to run this")
	}

	host, rawPort, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatalf("GTR_TEST_LDAP must be host:port, got %q: %v", address, err)
	}

	port, err := strconv.Atoi(rawPort)
	if err != nil {
		t.Fatalf("GTR_TEST_LDAP has a port that is not a number: %q", rawPort)
	}

	p := openWith(t, "KERBEROS_KEYTAB="+kerberostest.KeytabFile(t, tempdir.New(t), kerberosSecret))
	p.readyAdmin()

	// The seed's own values, as the integration suite's ldapSettings writes them.
	var status int

	p.run("point the installation at the seeded directory", chromedp.Evaluate(fmt.Sprintf(`
		(async () => {
			const csrf = document.cookie.split(';').map(c => c.trim())
				.find(c => c.startsWith('gtr_csrf='))?.slice('gtr_csrf='.length) ?? '';

			const r = await fetch('/api/v1/settings/ldap', {
				method: 'PUT',
				credentials: 'same-origin',
				headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
				body: JSON.stringify({
					enabled: true, host: %q, port: %d, startTls: false, useTls: false,
					bindDn: 'cn=admin,dc=example,dc=com', bindPassword: 'gtr-test-password',
					baseDn: 'dc=example,dc=com', userFilter: '(|(uid=%%s)(mail=%%s))',
					nameAttribute: 'cn', emailAttribute: 'mail', idAttribute: 'entryUUID',
					defaultRole: 'user',
				}),
			});

			return r.status;
		})()`, host, port), &status, awaitPromise))

	if status != http.StatusOK {
		t.Fatalf("saving the directory answered %d\n\napplication log:\n%s", status, p.app.Log())
	}

	p.signOutToANewTab()

	return p
}

// signOutToANewTab signs out and forgets what this tab remembers, which is where
// a person opening the page afresh starts: signing out holds the ticket back in
// the tab it happened in, and only one case here is about that.
func (p *page) signOutToANewTab() {
	p.t.Helper()

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID),
		chromedp.Evaluate(`sessionStorage.clear()`, nil))
}

// reloadSignedOut loads the sign-in screen again and waits until whatever the
// page does by itself there - trying a ticket, or not - has finished.
//
// Not reload(), which waits for a loaded interface that a page without a session
// never becomes. The ticket is asked for in the same turn that puts the form up,
// so a page at rest with the form showing has had its answer.
func (p *page) reloadSignedOut() {
	p.t.Helper()

	p.run("reload the sign-in screen", chromedp.Reload(),
		chromedp.WaitVisible("#form-login", chromedp.ByID))
	p.atRest()
}

// ticketWatch is what a page's ticket sign-ins looked like from the browser.
type ticketWatch struct {
	// signIns counts the requests to sign in with a ticket.
	signIns atomic.Int32

	// dismissed counts the times the browser wanted to ask for a user name and a
	// password instead, and was told no.
	dismissed atomic.Int32
}

// presentTickets has this page's browser send name's ticket with every ticket
// sign-in, the way a browser signed in to the domain does. At most most tickets
// are sent; a sign-in after that goes out without one.
//
// A fresh ticket each time, as a browser presents one: the service refuses an
// authenticator it has already seen. Issued in advance, because issuing one can
// fail the test, and a test may fail only from its own goroutine.
//
// A browser that has no ticket to send may ask for a user name and a password
// instead - Chrome on Windows does, for an address it does not trust with a
// ticket. Headless, nobody answers, and the request waited for good; measured on
// the first run of these cases. The dialog is dismissed here as a person would
// dismiss it, and counted.
func (p *page) presentTickets(t *testing.T, name string, most int) *ticketWatch {
	t.Helper()

	tickets := make(chan string, most)
	for range most {
		tickets <- kerberostest.Negotiate(t, kerberosSecret, name, kerberostest.Realm)
	}

	var watch ticketWatch

	executor := func() context.Context {
		return cdp.WithExecutor(p.ctx, chromedp.FromContext(p.ctx).Target)
	}

	chromedp.ListenTarget(p.ctx, func(event any) {
		switch e := event.(type) {
		case *fetch.EventAuthRequired:
			watch.dismissed.Add(1)

			// Off the listener, which answering from inside would block: the
			// answer travels over the connection that is delivering this event.
			go func() {
				_ = fetch.ContinueWithAuth(e.RequestID, &fetch.AuthChallengeResponse{
					Response: fetch.AuthChallengeResponseResponseCancelAuth,
				}).Do(executor())
			}()

		case *fetch.EventRequestPaused:
			go func() {
				headers := make([]*fetch.HeaderEntry, 0, len(e.Request.Headers)+1)
				for name, value := range e.Request.Headers {
					headers = append(headers, &fetch.HeaderEntry{Name: name, Value: fmt.Sprint(value)})
				}

				if e.Request.Method == http.MethodPost {
					watch.signIns.Add(1)

					select {
					case ticket := <-tickets:
						headers = append(headers, &fetch.HeaderEntry{Name: "Authorization", Value: ticket})
					default:
					}
				}

				_ = fetch.ContinueRequest(e.RequestID).WithHeaders(headers).Do(executor())
			}()
		}
	})

	p.run("present a ticket with every ticket sign-in", fetch.Enable().WithHandleAuthRequests(true).
		WithPatterns([]*fetch.RequestPattern{{URLPattern: "*/api/v1/auth/kerberos", RequestStage: fetch.RequestStageRequest}}))

	return &watch
}

// signedInAs is the address of the account this page's session belongs to, or
// nothing without one.
func (p *page) signedInAs() string {
	p.t.Helper()

	var email string

	p.run("ask who is signed in", chromedp.Evaluate(`
		(async () => {
			const r = await fetch('/api/v1/me', { credentials: 'same-origin' });
			return r.ok ? ((await r.json())?.data?.user?.email ?? '') : '';
		})()`, &email, awaitPromise))

	return email
}

// A tab that opens without a session signs itself in with the ticket and never
// shows the form. Signing out holds the ticket back, or reloading would sign
// straight back in and signing out would be something this page could not do;
// the button tries again when asked.
func TestATicketSignsThePageInAndSigningOutHoldsItBack(t *testing.T) {
	t.Parallel()

	p := ticketsReady(t)
	watch := p.presentTickets(t, "alice", 4)

	p.reload()
	p.waitSignedIn(t)

	if who := p.signedInAs(); who != "alice@example.com" {
		t.Fatalf("the ticket signed the page in as %q, want the directory's alice", who)
	}

	p.markTourSeen()
	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.reloadSignedOut()

	if n := watch.signIns.Load(); n != 1 {
		t.Errorf("after signing out, reloading tried the ticket again: %d ticket sign-ins, want 1", n)
	}

	if !p.visible("#login-kerberos") {
		t.Fatal("after signing out there is no button to sign in with the ticket again")
	}

	p.run("sign in with the ticket, asked for", p.click("#login-kerberos"))
	p.waitGone("#login-screen")
	p.waitSignedIn(t)

	if who := p.signedInAs(); who != "alice@example.com" {
		t.Errorf("the button signed the page in as %q", who)
	}
}

// A ticket stands in for the password and for nothing else. An account with a
// second factor is asked for the code on the same form, with nowhere to type an
// address or a password - the ticket has said who this is.
func TestATicketAsksForTheSecondFactorOnTheSameForm(t *testing.T) {
	t.Parallel()

	p := ticketsReady(t)
	watch := p.presentTickets(t, "alice", 4)

	p.reload()
	p.markTourSeen()
	secret := p.enableTOTP(t)
	p.signOutToANewTab()

	p.reloadSignedOut()

	if !p.visible("#login-totp-field") {
		t.Fatalf("a good ticket for an account with a second factor did not ask for the code; "+
			"the form says %q", p.text("#login-error"))
	}

	if p.visible(`#form-login input[name="email"]`) || p.visible(`#form-login input[name="password"]`) {
		t.Error("after a good ticket the form still asks for an address and a password")
	}

	if !p.visible("#login-kerberos-leave") {
		t.Error("the code step offers no way back to a password")
	}

	// The step after the one spent confirming the enrolment.
	code, err := security.TOTPCodeAt(secret, time.Now().Add(30*time.Second))
	if err != nil {
		t.Fatal(err)
	}

	p.run("give the code",
		chromedp.SendKeys(`#form-login input[name="totp"]`, code, chromedp.ByQuery),
		p.click(`#form-login button[type="submit"]`))
	p.waitGone("#login-screen")
	p.waitSignedIn(t)

	if who := p.signedInAs(); who != "alice@example.com" {
		t.Errorf("the ticket and its code signed the page in as %q", who)
	}

	// The first arrival, the one that asked for the code, and the one with it.
	if n := watch.signIns.Load(); n != 3 {
		t.Errorf("%d ticket sign-ins, want 3", n)
	}
}

// A browser without a ticket is left at the form with nothing said - the form is
// the answer - and is not asked again by itself in that tab. Asked with the
// button, it is told why nothing happened.
func TestWithoutATicketThePageIsLeftAtTheForm(t *testing.T) {
	t.Parallel()

	p := ticketsReady(t)
	watch := p.presentTickets(t, "alice", 0)
	p.reloadSignedOut()

	t.Logf("the browser wanted to ask for a password %d time(s)", watch.dismissed.Load())

	if p.visible("#login-error") {
		t.Errorf("a browser without a ticket was told %q before asking anything", p.text("#login-error"))
	}

	var held bool

	p.run("read what the tab remembers", chromedp.Evaluate(
		`sessionStorage.getItem('gtr.kerberos.heldBack') === '1'`, &held))

	if !held {
		t.Error("a ticket the browser could not present will be tried again at every reload")
	}

	p.run("ask for the ticket", p.click("#login-kerberos"))
	p.waitForText("#login-error", "Single sign-on did not work")
}

// Without a keytab the page offers no ticket and does not ask for one: no
// button, and not the one request that would put a Negotiate challenge in front
// of a browser. Needs no directory, so the browser job runs it too.
func TestWithoutAKeytabNoTicketIsTried(t *testing.T) {
	t.Parallel()

	p := open(t)
	watch := p.presentTickets(t, "alice", 1)

	// As a new tab. The first load ran before anything was watching, and a ticket
	// it tried and failed would be held back in this one - so a reload would try
	// nothing whether or not the page asks without an offer.
	p.run("forget this tab's memory", chromedp.Evaluate(`sessionStorage.clear()`, nil))
	p.reloadSignedOut()

	if p.visible("#login-kerberos") {
		t.Error("an installation without a keytab offers a ticket sign-in")
	}

	if n := watch.signIns.Load(); n != 0 {
		t.Errorf("an installation without a keytab tried the ticket %d time(s)", n)
	}
}
