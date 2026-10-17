//go:build browser

package browser

import (
	"fmt"
	"testing"

	"github.com/chromedp/chromedp"
)

// A table with ordinary data fits its card: its free text folds onto further
// lines, and its row's actions onto further lines between them, rather than the
// table scrolling sideways with the actions out of sight.
//
// Every cell was held to one line so that a table scrolled inside its box rather
// than widening the page. That kept the page still and pushed the controls out of
// the card: a long name or description put "bearbeiten" and "löschen" past the
// right-hand edge at 1,024 pixels and at 1,600, and once the accounts table could
// show which role a row holds, German needed 170 pixels more than the card has
// even with short names. Text and actions wrap now, each action kept whole;
// decided on 2026-10-17.
func TestATableWithOrdinaryDataFitsItsCard(t *testing.T) {
	t.Parallel()

	const (
		long        = "Maximiliane Freifrau von und zu Beispielhausen"
		description = "Abstimmung mit dem Kunden über die Anforderungen an die Schnittstelle zum Buchhaltungssystem"
	)

	p := open(t)
	p.readyAdmin()

	var status string

	p.run("create two accounts", chromedp.Evaluate(`(async () => {
		const csrf = document.cookie.split(';').map(c => c.trim())
			.find(c => c.startsWith('gtr_csrf='))?.slice('gtr_csrf='.length) ?? '';
		const headers = { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf };
		const out = [];

		for (const [name, email, role] of [['`+long+`', 'long@example.com', 'user-admin'], ['Bo', 'bo@example.com', 'user']]) {
			const r = await fetch('/api/v1/users', { method: 'POST', credentials: 'same-origin', headers,
				body: JSON.stringify({ name, email, role, password: 'a-long-enough-password' }) });
			out.push(r.status);
		}

		return out.join(' ');
	})()`, &status, awaitPromise))

	if status != "201 201" {
		t.Fatalf("could not create the accounts: HTTP %s", status)
	}

	p.chooseLanguage("de")

	check := func(where, table string) {
		t.Helper()

		var reading struct {
			Over     float64  `json:"over"`
			Unseen   []string `json:"unseen"`
			Broken   []string `json:"broken"`
			Controls int      `json:"controls"`
		}

		p.evalJSON(`JSON.stringify((() => {
			const table = document.querySelector('`+table+`');
			const wrap = table.closest('.table-wrap');
			const seen = wrap.getBoundingClientRect();
			const controls = [...table.querySelectorAll('tbody td.actions button, tbody td.actions a')]
				.filter((control) => control.getBoundingClientRect().height > 0);
			const line = (control) => parseFloat(getComputedStyle(control).lineHeight) || 18;

			return {
				over: table.scrollWidth - wrap.clientWidth,
				unseen: controls.filter((control) => control.getBoundingClientRect().right > seen.right + 1)
					.map((control) => control.textContent.trim()),
				broken: controls.filter((control) => control.getBoundingClientRect().height > line(control) * 1.6)
					.map((control) => control.textContent.trim()),
				controls: controls.length,
			};
		})())`, &reading)

		if reading.Controls == 0 {
			t.Fatalf("%s: the table has no actions to look at", where)
		}

		if reading.Over > 1 {
			t.Errorf("%s: the table is %.0fpx wider than its card", where, reading.Over)
		}

		if len(reading.Unseen) > 0 {
			t.Errorf("%s: actions past the edge of the card: %q", where, reading.Unseen)
		}

		if len(reading.Broken) > 0 {
			t.Errorf("%s: actions broken over two lines: %q", where, reading.Broken)
		}
	}

	// Not at 1,024 pixels: there the accounts table needs more than its card in the
	// faces the CI runner draws with - the longest German role name in its dropdown
	// and "Passwort zurücksetzen" kept whole - and scrolls inside its box.
	for _, width := range []int64{1600, 1280} {
		p.run(fmt.Sprintf("%dpx", width), chromedp.EmulateViewport(width, 900))
		p.run("open the accounts", p.click(`.tab[data-view="users"]`),
			chromedp.Evaluate(`refreshAll()`, nil, awaitPromise))
		p.waitShown("#table-users tbody td.actions")

		check(fmt.Sprintf("the accounts at %dpx", width), "#table-users")
	}

	// And somebody's own entries, with a description as long as the ones people
	// write.
	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))
	p.signIn("bo@example.com", "a-long-enough-password")
	p.waitGone("#login-screen")
	p.settleWelcome()

	p.run("book with a long description", chromedp.Evaluate(`(async () => {
		const csrf = document.cookie.split(';').map(c => c.trim())
			.find(c => c.startsWith('gtr_csrf='))?.slice('gtr_csrf='.length) ?? '';
		const now = new Date();
		const today = now.getFullYear() + '-' + String(now.getMonth() + 1).padStart(2, '0') + '-' + String(now.getDate()).padStart(2, '0');

		await fetch('/api/v1/timesheets', { method: 'POST', credentials: 'same-origin',
			headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
			body: JSON.stringify({ date: today, durationHours: 2.5, description: '`+description+`' }) });

		await refreshAll();
		return 1;
	})()`, nil, awaitPromise))

	p.chooseLanguage("de")

	for _, width := range []int64{1600, 1280, 1024} {
		p.run(fmt.Sprintf("%dpx", width), chromedp.EmulateViewport(width, 900))
		p.run("open the entries", p.click(`.tab[data-view="timesheets"]`))
		p.waitShown("#table-timesheets tbody td.actions")

		check(fmt.Sprintf("the entries at %dpx", width), "#table-timesheets")
	}
}
