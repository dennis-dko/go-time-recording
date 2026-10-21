//go:build browser

package browser

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/dennis-dko/go-time-recording/test/oidctest"
)

// Every way to sign in keeps its words on one line where the card has room for
// them.
//
// The buttons sat in one row under the fields, made for two: signing in, and a
// passkey beside it. A provider to sign in through makes three, and three in the
// card's width squeezed the two beside the submit button until their words ran
// to three lines each - "Sign in / through your / organisation" - with the
// window's whole height to spare below them. Nothing was cut and nothing was
// wider than the screen, so no check that looks for those said anything; it was
// seen in a picture taken for the README.
//
// Counted in lines of words rather than measured in widths, because a squeezed
// button is still inside its row. The provider is left unnamed, which gives the
// form its longest words, and both languages are asked because German's are
// longer still.
func TestEveryWayToSignInKeepsItsWordsOnOneLine(t *testing.T) {
	t.Parallel()

	provider := oidctest.Start(t)

	p := open(t)
	p.readyAdmin()
	p.awaitJSON(`api('/settings/openid', {method: 'PUT', body: JSON.stringify({
		enabled: true, issuer: `+strconv.Quote(provider.URL)+`, clientId: `+strconv.Quote(oidctest.ClientID)+`,
		clientSecret: `+strconv.Quote(oidctest.ClientSecret)+`, label: ''})}).then(() => true)`, nil)

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	for _, language := range []string{"en", "de"} {
		if language == "de" {
			p.run("a German browser", speakingGerman())
			p.reloadSignedOut()
		}

		p.waitShown("#login-openid")
		p.waitShown("#login-passkey")

		for _, width := range []int64{1440, 1024, 901} {
			p.run(fmt.Sprintf("%dpx", width), chromedp.EmulateViewport(width, 900))

			var lines map[string]int

			p.evalJSON(`(() => {
				const linesOf = `+linesOfWords+`;

				return JSON.stringify(Object.fromEntries([...document.querySelectorAll('.login-actions button')]
					.filter((button) => !button.hidden)
					.map((button) => [button.textContent.trim(), linesOf(button)])));
			})()`, &lines)

			if len(lines) < 3 {
				t.Fatalf("in %s at %dpx the form offers %v; this asks about three ways to sign in",
					language, width, lines)
			}

			for words, count := range lines {
				if count != 1 {
					t.Errorf("in %s at %dpx %q runs to %d lines", language, width, words, count)
				}
			}
		}
	}
}
