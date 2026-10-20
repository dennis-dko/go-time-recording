//go:build browser

package browser

import (
	"strconv"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/dennis-dko/go-time-recording/test/oidctest"
)

// configureTheProvider has the signed-in administrator point the installation at
// a provider, as the Settings screen does - after the page opened, so that what
// the page asked at its start says nothing is offered.
func (p *page) configureTheProvider(provider *oidctest.Provider) {
	p.t.Helper()

	p.awaitJSON(`api('/settings/openid', {
		method: 'PUT', body: JSON.stringify({
			enabled: true, issuer: `+strconv.Quote(provider.URL)+`, clientId: `+strconv.Quote(oidctest.ClientID)+`,
			clientSecret: `+strconv.Quote(oidctest.ClientSecret)+`, label: 'Test provider',
		}),
	}).then(() => true)`, nil)
}

// The built-in administrator's own card says, on the next load of the screen, why
// it has nothing to press - rather than being missing from a screen that then
// looks as if it lost something - and offers no link the server would refuse.
func TestTheBuiltInAdministratorIsToldWhyItHasNoProviderLink(t *testing.T) {
	t.Parallel()

	provider := oidctest.Start(t)

	p := open(t)
	p.readyAdmin()
	p.configureTheProvider(provider)

	p.awaitJSON(`refreshAll().then(() => true)`, nil)

	p.run("open my account", p.click(`.tab[data-view="settings"]`),
		chromedp.WaitVisible("#openid-card", chromedp.ByID))

	p.waitForText("#openid-state", "The built-in administrator signs in with its password")

	if p.visible("#openid-link") || p.visible("#openid-unlink") {
		t.Error("the built-in administrator is offered a link the server refuses it")
	}
}

// Somebody links their account under My account by going to the provider and
// coming back, and from then on the sign-in form's button for the provider signs
// them in: the browser leaves for the provider and arrives back signed in, with
// the code it came back with gone from the address.
func TestAnAccountLinkedToAProviderSignsInThroughIt(t *testing.T) {
	t.Parallel()

	provider := oidctest.Start(t)

	p := open(t)
	p.readyAdmin()
	p.configureTheProvider(provider)
	p.createOrdinaryAccount(t, workerEmail, workerPassword)

	// Configured after the page opened, so it is the sign-out that has to learn of
	// it - the page's own start asked before there was anything to offer.
	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.waitForText("#login-openid", "Sign in with Test provider")

	p.signIn(workerEmail, workerPassword)
	p.waitGone("#login-screen")
	p.settled()
	p.settleWelcome()

	p.run("open my account", p.click(`.tab[data-view="settings"]`),
		chromedp.WaitVisible("#openid-card", chromedp.ByID))

	p.waitForText("#openid-state", "Not linked yet.")

	if label := p.text("#openid-link"); label != "Link with Test provider" {
		t.Errorf("the card offers %q, want the provider named", label)
	}

	p.run("link the account", p.click("#openid-link"))

	// Away to the provider and back, which loads the page again.
	p.waitForText("#openid-state", "Linked since")
	p.waitForText("#toast", "Your account is linked")

	var address string

	p.run("read the address", chromedp.Evaluate(`location.pathname + location.search`, &address))

	if address != "/" {
		t.Errorf("the address still reads %q after the way back, want the code and the state gone", address)
	}

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.waitForText("#login-openid", "Sign in with Test provider")

	p.run("sign in through the provider", p.click("#login-openid"))

	p.waitGone("#login-screen")
	p.settled()

	// The account becomeWorker made, which the round linked - named on the bar,
	// which it can only be from a session the provider's round opened.
	p.waitForText("#who", "Erika")
}
