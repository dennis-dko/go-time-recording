//go:build browser

package browser

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// Signing out takes the previous account's project names off the screen.
//
// forgetTheLastAccount empties what a loader filled, and states the rule: "a
// table whose loader returns early keeps whatever it last held". Every loader
// here begins by checking a right and returning if it is absent, so what was
// loaded for the last account stays until something else takes it away.
//
// It empties `table[id] tbody`. The project lists are not tables - loadProjects
// fills four <select>s through fillSelect: the stopwatch's, the booking form's,
// the report form's and the entry filter's. Nothing emptied those, so the
// previous account's project names stayed in them.
//
// Reached by an account that may book time and may not read projects, which the
// role editor allows - it offers every permission separately. loadProjects
// returns at its first line for such an account, so the dropdown it never filled
// is still showing the last person's work.
//
// Project names are private here. CLAUDE.md puts it plainly: a project belongs to
// exactly one person, and the reason the record was hidden in the first place is
// that "opening a report was enough to learn what somebody had recorded against
// their own private category".
//
// The invariant is simpler than the way in, so that is what this checks: signing
// out clears them. The custom role is the consequence, not the rule.
func TestSigningOutClearsTheProjectListsOnScreen(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	const named = "Cathedral Roof"

	p.run("create a project", chromedp.Evaluate(`(async () => {
		const token = document.cookie.split('; ')
			.find((c) => c.startsWith('gtr_csrf=')).split('=')[1];
		const res = await fetch('/api/v1/projects', {
			method: 'POST',
			credentials: 'same-origin',
			headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': token },
			body: JSON.stringify({ name: '`+named+`', startDate: '2026-08-03' }),
		});
		return res.status;
	})()`, nil, awaitPromise))

	// Reloaded, because the lists were filled before the project existed.
	p.run("pick it up",
		chromedp.Reload(),
		chromedp.WaitVisible(`html[data-loaded="yes"]`, chromedp.ByQuery))

	var before string

	p.run("the list holds it", chromedp.Evaluate(
		`document.querySelector('#timer-project').textContent`, &before))

	if !strings.Contains(before, named) {
		t.Fatalf("the project list does not hold %q, so this case would pass "+
			"whatever the sign-out does. It holds: %q", named, before)
	}

	p.run("sign out", p.click("#logout"),
		chromedp.WaitVisible("#login-screen", chromedp.ByID))

	var after string

	p.run("read it after signing out", chromedp.Evaluate(
		`document.querySelector('#timer-project').textContent`, &after))

	if strings.Contains(after, named) {
		t.Errorf("after signing out the project list still names %q. An account "+
			"that may book time and may not read projects sees it: loadProjects "+
			"returns at its first line, and nothing emptied what the last one left",
			named)
	}
}
