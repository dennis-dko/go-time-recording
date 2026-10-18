//go:build browser

package browser

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// The role in a user's row can be read, and two roles told apart.
//
// The dropdown that changes it took the width every field takes, all of its cell,
// and a select sized as a share of its cell adds nothing to how wide a table makes
// the column. So the column came out as wide as "Administrator" in the built-in
// account's row, and the dropdown cut its own label short: in German the two roles
// anybody is likely to compare, Benutzer and Benutzer & Administrator, both read
// "Benutze".
//
// Measured against a dropdown showing nothing but the role's name, in the same
// face, so what is asked is that the name fits - not some width in pixels.
func TestTheRoleInAUsersRowCanBeRead(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.createAccount(t, "bo@example.com", "a-long-enough-password", "user")
	p.createAccount(t, "cleo@example.com", "a-long-enough-password", "user-admin")

	p.run("a desktop", chromedp.EmulateViewport(1600, 900))
	p.chooseLanguage("de")

	p.run("open the accounts", p.click(`.tab[data-view="users"]`),
		chromedp.WaitVisible("#table-users", chromedp.ByID),
		chromedp.Evaluate(`refreshAll()`, nil, awaitPromise))

	var rows int

	p.run("count the dropdowns", chromedp.Evaluate(
		`document.querySelectorAll('#table-users tbody select').length`, &rows))

	if rows != 2 {
		t.Fatalf("expected a role dropdown in each of the two new accounts' rows, found %d", rows)
	}

	var cut []string

	p.evalJSON(`JSON.stringify([...document.querySelectorAll('#table-users tbody select')].map((select) => {
		const has = select.getBoundingClientRect().width;
		const name = select.selectedOptions[0].textContent.split(' — ')[0];

		// Outside the table, so measuring it cannot widen the column it measures.
		const probe = document.createElement('select');
		probe.append(new Option(name));
		probe.style.cssText = 'position: absolute; visibility: hidden; width: auto';
		probe.style.font = getComputedStyle(select).font;
		document.body.append(probe);

		const needed = probe.getBoundingClientRect().width;
		probe.remove();

		return has + 0.5 < needed ? name + ': ' + Math.round(has) + 'px of the ' + Math.round(needed) + ' it needs' : '';
	}).filter(Boolean))`, &cut)

	if len(cut) > 0 {
		t.Errorf("a role dropdown in the accounts table is narrower than its own role's name, "+
			"so the name is cut: %s", strings.Join(cut, "; "))
	}
}
