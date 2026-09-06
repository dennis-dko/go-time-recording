//go:build browser

package browser

import (
	"encoding/json"
	"testing"

	"github.com/chromedp/chromedp"
)

// A revealed password does not stay revealed for the next person.
//
// wirePasswordReveal says when it goes back: "The field goes back to being
// hidden when the form is submitted or the page is left, because the browser is
// where it stays visible otherwise: nothing here re-renders these inputs."
//
// Submitting is handled. Leaving is not - not by switching screens and not by
// signing out. handBackTheScreen resets every form, which empties the value and
// does nothing to the type, so the box is left standing as a text field with its
// button still saying "Hide the password".
//
// The next person at that desk then types their password into a box that shows
// it. That is the machine handBackTheScreen exists for: it clears the drafts, the
// forms, the loose fields, the caches and three kinds of credential precisely
// because the next sign-in may be somebody else.
func TestARevealedPasswordIsHiddenAgainWhenTheSessionEnds(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.run("open My account", p.click(`.tab[data-view="settings"]`),
		chromedp.WaitVisible("#form-password", chromedp.ByID))

	p.run("type a password and look at it", chromedp.Evaluate(`(() => {
		const field = document.querySelector('#form-password input[name="newPassword"]');

		field.value = 'a-password-being-transcribed';
		field.dispatchEvent(new Event('input', { bubbles: true }));

		field.parentElement.querySelector('button.password-toggle').click();

		return field.type;
	})()`, nil))

	read := func(when string) (kind string, label string) {
		t.Helper()

		var out string

		p.run("read the field "+when, chromedp.Evaluate(`(() => {
			const field = document.querySelector('#form-password input[name="newPassword"]');
			const button = field.parentElement.querySelector('button.password-toggle');

			return JSON.stringify({
				kind: field.type,
				label: button.getAttribute('aria-label') ?? '',
			});
		})()`, &out))

		var field struct {
			Kind  string `json:"kind"`
			Label string `json:"label"`
		}

		if err := json.Unmarshal([]byte(out), &field); err != nil {
			t.Fatalf("reading the field (%q): %v", out, err)
		}

		return field.Kind, field.Label
	}

	kind, _ := read("while looking at it")

	if kind != "text" {
		t.Fatalf("the password is not revealed (type %q), so this case would pass "+
			"whatever the sign-out does", kind)
	}

	p.run("sign out", p.click("#logout"),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	after, label := read("after signing out")

	if after != "password" {
		t.Errorf("the field is still a %q box after the session ended, so whoever "+
			"signs in next types their password into one that shows it", after)
	}

	if label == "Hide the password" {
		t.Errorf("the button still offers to hide the password (%q), which is the "+
			"label for a field that is showing one", label)
	}
}
