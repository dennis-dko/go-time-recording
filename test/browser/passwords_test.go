//go:build browser

package browser

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// The button is added by the script, to fields written in the markup, so
// whether it exists at all is a question only a browser can answer - and what it
// does is a change to an attribute that no API test can see.
func TestAPasswordCanBeRevealedAndHiddenAgain(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-datasource", chromedp.ByID))
	p.waitForFilled("#datasource-active")

	// A database on a server, because that is where a database password exists.
	// The card opens on whatever this process is connected to, which here is a
	// file - and a file has no host, no user and nothing to authenticate with, so
	// those fields are put away and there is no password to reveal.
	p.run("choose a server", p.chooseOption(`#form-datasource select[name="dialect"]`, "postgres"))

	const field = `#form-datasource input[name="password"]`

	// Every password field gets one, so this one stands for the rest.
	if !p.visible(`#form-datasource .password-toggle`) {
		t.Fatal("the database password has no reveal button")
	}

	p.run("type a password", chromedp.SendKeys(field, "not-a-real-password", chromedp.ByQuery))

	if got := p.attr(field, "type"); got != "password" {
		t.Errorf("the field starts as %q, want it hidden", got)
	}

	p.run("reveal", p.click(`#form-datasource .password-toggle`))

	if got := p.attr(field, "type"); got != "text" {
		t.Errorf("after pressing the button the field is %q, so nothing was revealed", got)
	}

	// The value survived the type change; a reveal that emptied the field would
	// be worse than none.
	if got := p.value(field); got != "not-a-real-password" {
		t.Errorf("revealing changed the value to %q", got)
	}

	p.run("hide again", p.click(`#form-datasource .password-toggle`))

	if got := p.attr(field, "type"); got != "password" {
		t.Errorf("the field stayed %q after pressing the button a second time", got)
	}
}

// Every password field has exactly one eye, and it says which state it is in.
//
// There were two implementations of this in one page - one wrapping the field in
// .pw-wrap with a .pw-toggle, the other in .password-field with a
// .password-toggle - and each guarded only against itself running twice. So
// every password field got both: two buttons, one drawn on top of the other, and
// the eye came out looking like two overlapping eyes. Reported from Firefox and
// true everywhere.
//
// The one that stayed is the one that says what it is doing: pressed or not,
// labelled in the reader's language, struck through while the password is
// showing, and put back to hidden when the form is submitted.
func TestEveryPasswordFieldHasExactlyOneEye(t *testing.T) {
	t.Parallel()

	p := open(t)

	count := func(where string) string {
		var out string

		p.run("count the eyes "+where, chromedp.Evaluate(`
			(() => {
				const fields = [...document.querySelectorAll('.password-field')];

				return JSON.stringify({
					fields: fields.length,
					worst: fields.reduce((n, f) => Math.max(n, f.querySelectorAll('button').length), 0),
					bare: fields.filter((f) => f.querySelectorAll('button').length === 0).length,
					strays: document.querySelectorAll('.pw-toggle, .pw-wrap').length,
				});
			})()`, &out))

		return out
	}

	// The sign-in screen first: it is the one password field somebody meets
	// before anything else has run.
	if got := count("on the sign-in screen"); !strings.Contains(got, `"worst":1`) {
		t.Errorf("the sign-in password field does not have exactly one eye: %s", got)
	}

	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-datasource", chromedp.ByID))
	p.waitForFilled("#datasource-active")

	// A server, so the connection's own password field is on screen too.
	p.run("choose a server", p.chooseOption(`#form-datasource select[name="dialect"]`, "postgres"))

	got := count("across the screens")

	if !strings.Contains(got, `"worst":1`) {
		t.Errorf("some password field has more or fewer than one eye: %s", got)
	}

	if !strings.Contains(got, `"bare":0`) {
		t.Errorf("some password field has no eye at all: %s", got)
	}

	if !strings.Contains(got, `"strays":0`) {
		t.Errorf("the second implementation is still on the page: %s", got)
	}

	// And it says which state it is in, rather than only changing the field.
	var state string

	p.run("press it", chromedp.Evaluate(`
		(() => {
			const button = document.querySelector('#form-datasource .password-toggle');
			const input = document.querySelector('#form-datasource input[name="password"]');

			const before = {
				pressed: button.getAttribute('aria-pressed'),
				revealed: button.classList.contains('revealed'),
				type: input.type,
			};

			button.click();

			return JSON.stringify({
				before,
				after: {
					pressed: button.getAttribute('aria-pressed'),
					revealed: button.classList.contains('revealed'),
					type: input.type,
				},
			});
		})()`, &state))

	for _, want := range []string{
		`"before":{"pressed":"false","revealed":false,"type":"password"}`,
		`"after":{"pressed":"true","revealed":true,"type":"text"}`,
	} {
		if !strings.Contains(state, want) {
			t.Errorf("pressing the eye did not go from hidden to shown as %s: %s", want, state)
		}
	}
}
