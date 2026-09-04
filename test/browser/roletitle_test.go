//go:build browser

package browser

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// The role form says which role it has open, in either language, and goes on
// saying it after the language changes.
//
// Two faults meet on this heading. Its English fallback is "Edit role" while the
// German is "Rolle „{0}" bearbeiten" and the caller interpolates the name into
// both - so the name reached German readers and not English ones, on the one
// screen where the form is shared between every role and the heading is what
// says which one is open. Its sibling one line above, role.view, names the role
// in English.
//
// And the heading keeps the data-i18n the markup gave it, which names the
// creating key, so a language change put "Create role" back over it. That cannot
// be fixed the way the other form titles were: this message carries a value, and
// declaring its key would have applyLanguage put the pattern back with its {0}
// showing. It is drawn again through redrawable instead.
func TestTheRoleFormNamesTheRoleItHasOpen(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	// A role of this installation's own, because the shipped ones are opened with
	// role.view - which already names them in both languages. role.edit is the
	// one whose English lost the name, and only a role somebody made reaches it.
	const named = "Cellar keeper"

	p.run("open the roles", p.click(`.tab[data-view="roles"]`),
		chromedp.WaitVisible(`#form-role`, chromedp.ByID))
	p.settled()

	p.run("name it", chromedp.SendKeys(
		`#form-role input[name="name"]`, named, chromedp.ByQuery))

	// A role with no rights is refused, and which rights they are is not what
	// this case is about.
	p.run("tick two rights", chromedp.Evaluate(`(() => {
		for (const box of [...document.querySelectorAll(
			'#permission-list input[name=permissions]')].slice(0, 2)) {
			box.checked = true;
			box.dispatchEvent(new Event('input', { bubbles: true }));
			box.dispatchEvent(new Event('change', { bubbles: true }));
		}

		return true;
	})()`, nil))

	p.run("save it", p.click(`#form-role button[type="submit"]`))

	p.waitForText("#table-roles tbody", named)

	p.run("open it", chromedp.Evaluate(`(() => {
		const row = [...document.querySelectorAll('#table-roles tbody tr')]
			.find((r) => r.textContent.includes('`+named+`'));

		row.querySelector('.actions button.link').click();

		return true;
	})()`, nil))

	p.run("the form is open", chromedp.WaitVisible(`#form-role`, chromedp.ByID))

	title := func(when string) string {
		t.Helper()

		var out string

		p.run("read the heading "+when, chromedp.Evaluate(
			`document.querySelector('#role-form-title').textContent.trim()`, &out))

		return out
	}

	english := title("in English")

	if !strings.Contains(english, named) {
		t.Errorf("the heading reads %q and does not name the role %q it has open. "+
			"The German for the same key names it, and so does role.view beside it",
			english, named)
	}

	// A brace on screen would mean the pattern was put back untouched.
	if strings.Contains(english, "{0}") {
		t.Errorf("the heading reads %q, with the placeholder showing", english)
	}

	p.chooseLanguage("de")

	german := title("in German")

	if !strings.Contains(german, named) || strings.Contains(german, "{0}") {
		t.Errorf("after switching to German the heading reads %q; it should still "+
			"name %q and show no placeholder", german, named)
	}

	// The creating heading, for comparison: the one the markup's own key would
	// have put back over it.
	p.run("start a new role", p.click(`#role-reset`))

	creating := title("while creating, in German")

	if german == creating {
		t.Errorf("after the language change the heading reads %q, which is what it "+
			"says when creating a role - while the form had %q open", german, named)
	}
}
