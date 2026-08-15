//go:build browser

package browser

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// The update card says something true, and what it says differs by deployment.
//
// A single binary can fetch its successor and put it in its own place; a
// container cannot, because the next recreate undoes it. So the card either
// offers a button or says what to run instead - and getting that wrong means
// offering an update that silently reverts.
func TestTheUpdateCardOffersWhatThisDeploymentCanDo(t *testing.T) {
	// Our own feed: the point is what the card does with an answer, not whether
	// GitHub is up.
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v99.0.0","body":"much better",` +
			`"html_url":"https://example.invalid/release","assets":[]}`))
	}))
	defer feed.Close()

	p := openWith(t, "UPDATE_FEED="+feed.URL)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#update-card", chromedp.ByID))

	state := p.text("#update-state")
	t.Logf("Karte sagt: %q", state)

	if state == "" {
		t.Error("the update card says nothing at all")
	}

	// This build was not made from a release, and the card has to say that rather
	// than call an unrankable version the newest one.
	if strings.Contains(state, "newest") {
		t.Errorf("the card claims %q, but this build cannot be ranked at all", state)
	}

	if hint := p.text("#update-hint"); !strings.Contains(hint, "v99.0.0") {
		t.Errorf("the card does not name the published version: %q", hint)
	}

	// This build calls itself "dev", which cannot be compared - so no button.
	if p.visible("#update-now") {
		t.Error("an update is offered from a release that published no binary for " +
			"this platform")
	}

	// The link to the release's own words, so nobody has to take the
	// application's word for what it would install.
	if !p.visible("#update-notes") {
		t.Error("the card does not link to the release itself")
	}

}

// Somebody who administers but is not that kind of administrator gets no card at
// all, rather than a card that refuses when pressed.
func TestTheUpdateCardIsNotOfferedToTheCombinedRole(t *testing.T) {
	p := open(t)
	p.readyAdmin()
	p.createAccount(t, "bothe@example.com", "both-jobs-password-1", "user-admin")

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn("bothe@example.com", "both-jobs-password-1")
	p.waitGone("#login-screen")
	p.settleWelcome()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-branding", chromedp.ByID))

	if p.visible("#update-card") {
		t.Error("the combined role is offered an update it would be refused")
	}

	// The rest of the screen is theirs, so this is the one card missing rather
	// than the screen not having loaded.
	if !p.visible("#form-branding") {
		t.Error("the appearance card is missing too, so the screen did not load")
	}
}

// Switched off, the card says so rather than saying nothing.
func TestTheUpdateCardSaysWhenCheckingIsOff(t *testing.T) {
	p := openWith(t, "UPDATE_CHECK=false")
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#update-card", chromedp.ByID))

	hint := p.text("#update-hint")
	t.Logf("Hinweis: %q", hint)

	if !strings.Contains(hint, "UPDATE_CHECK") {
		t.Errorf("the card says %q, which does not name the setting that turned it off",
			hint)
	}

	if p.visible("#update-now") {
		t.Error("an update is offered although checking is switched off")
	}
}
