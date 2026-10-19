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

// On a telephone a table keeps its row's actions in sight, and its text readable.
//
// A table too wide for a telephone scrolls inside its card, and every table with
// actions was: edit, delete and reset a password lay past the right-hand edge of
// the timesheets, the projects, the accounts and the roles at 360 and 430 pixels,
// and of the accounts on a tablet held upright, reached only by a swipe nothing
// announced. The actions now stay at the card's right-hand edge while the other
// columns pass beneath them, and text keeps a width of its own rather than
// folding to its longest word - a name had stood as a column of single words
// beside actions that were out of sight anyway. Decided on 2026-10-19.
func TestOnAPhoneATableKeepsItsActionsInSight(t *testing.T) {
	t.Parallel()

	const long = "Maximiliane Alexandra von Hohenstein-Wittgenstein"

	p := open(t)
	p.readyAdmin()

	var status string

	p.run("create an account with a long name", chromedp.Evaluate(`(async () => {
		const csrf = document.cookie.split(';').map(c => c.trim())
			.find(c => c.startsWith('gtr_csrf='))?.slice('gtr_csrf='.length) ?? '';
		const r = await fetch('/api/v1/users', { method: 'POST', credentials: 'same-origin',
			headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
			body: JSON.stringify({ name: '`+long+`', email: 'lang@example.com', role: 'user', password: 'a-long-enough-password' }) });
		return String(r.status);
	})()`, &status, awaitPromise))

	if status != "201" {
		t.Fatalf("could not create the account: HTTP %s", status)
	}

	p.chooseLanguage("de")

	for _, size := range []struct{ w, h int64 }{{360, 780}, {430, 932}, {768, 1024}} {
		p.run(fmt.Sprintf("%dx%d", size.w, size.h), chromedp.EmulateViewport(size.w, size.h))

		for _, view := range []string{"users", "roles"} {
			p.run("open "+view, chromedp.Evaluate(`switchView('`+view+`'); refreshAll()`, nil, awaitPromise))
			p.atRest()

			var reading struct {
				Missing string   `json:"missing"`
				Hidden  []string `json:"hidden"`
				Lines   float64  `json:"lines"`
			}

			p.evalJSON(`JSON.stringify((() => {
				const table = document.querySelector('#view-`+view+` table');
				const wrap = table?.closest('.table-wrap');
				if (!wrap) return { missing: 'no table' };

				const hidden = [...table.querySelectorAll('tbody td.actions button')].filter((button) => {
					// Brought up the window, and only up it: scrolling the table sideways
					// would bring a hidden action into view by itself.
					const before = button.getBoundingClientRect();
					window.scrollTo(0, window.scrollY + before.top - window.innerHeight / 2);

					const seen = wrap.getBoundingClientRect();
					const r = button.getBoundingClientRect();
					const hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);

					return r.height > 0 && (r.right > seen.right + 1 || r.left < seen.left - 1 || !(hit === button || button.contains(hit)));
				}).map((button) => button.textContent.trim());

				// The lines of the name itself: a cell is as tall as its row, and the
				// row is as tall as its tallest cell.
				const name = [...table.querySelectorAll('tbody td')].find((cell) => cell.textContent.trim() === '`+long+`');
				let lines = 0;

				if (name) {
					const range = document.createRange();
					range.selectNodeContents(name);
					lines = new Set([...range.getClientRects()].map((r) => Math.round(r.top))).size;
				}

				return { missing: '', hidden, lines };
			})())`, &reading)

			if reading.Missing != "" {
				t.Fatalf("%dx%d, %s: %s", size.w, size.h, view, reading.Missing)
			}

			if len(reading.Hidden) > 0 {
				t.Errorf("at %dx%d the %s table hides actions past its edge or under its cells: %q",
					size.w, size.h, view, reading.Hidden)
			}

			// One word to a line is the column the name used to stand as; it has five
			// places to fold - four spaces and a hyphen - and so five words.
			if view == "users" && reading.Lines >= 5 {
				t.Errorf("at %dx%d the name stands one word to a line, %.0f lines, in the accounts table",
					size.w, size.h, reading.Lines)
			}
		}
	}

	// And the edge of the pinned actions is drawn where tables scroll beneath them.
	// Without it a name passing underneath ended mid-letter - "Alexandra vo" - and
	// read as a typing error rather than as text that goes on. A wide screen, where
	// the tables fit, keeps the plain column it had.
	for _, edge := range []struct {
		w, h  int64
		drawn bool
	}{{390, 844, true}, {1600, 900, false}} {
		var shadow string

		p.run(fmt.Sprintf("%dx%d", edge.w, edge.h), chromedp.EmulateViewport(edge.w, edge.h),
			chromedp.Evaluate(`getComputedStyle(document.querySelector('#table-users tbody td.actions')).boxShadow`, &shadow))

		if drawn := shadow != "none"; drawn != edge.drawn {
			t.Errorf("at %dx%d the pinned actions' edge is %q, where it should be drawn: %t", edge.w, edge.h, shadow, edge.drawn)
		}
	}
}
