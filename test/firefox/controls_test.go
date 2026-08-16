//go:build firefox

package firefox

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/test/harness"
)

func TestMain(m *testing.M) {
	cleanup, err := harness.Build()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	code := m.Run()

	cleanup()
	os.Exit(code)
}

// The tick boxes that select rows are actually on screen in Firefox.
//
// They were not, and for as long as bulk deletion had existed. The column was in
// the table, the boxes were in the cells, the bar above the table worked the
// moment anything was ticked - and the boxes were 0px wide, so there was nothing
// to tick. Every assertion the Chrome suite makes about bulk deletion held while
// this was true, because those assertions ask the DOM what is there and the DOM
// was right.
//
// So this asks for the measurement instead. A control nobody can see is not a
// control, whatever the tree says.
func TestTheSelectionBoxesAreVisibleInFirefox(t *testing.T) {
	app := harness.Start(t)
	b := openBrowser(t)

	b.goTo(app.BaseURL())
	signIn(t, b)

	// Somebody to select. The built-in administrator cannot be deleted, so a table
	// holding only it has a column and no boxes in it.
	createAccount(t, b, "sven@example.com")

	b.goTo(app.BaseURL())
	b.settle()

	b.eval(`(() => { document.querySelector('.tab[data-view="users"]').click(); return 1; })()`)
	b.settle()

	var table struct {
		Rows     int `json:"rows"`
		Boxes    int `json:"boxes"`
		Smallest struct {
			Width  float64 `json:"width"`
			Height float64 `json:"height"`
		} `json:"smallest"`
	}

	b.evalJSON(`(() => {
		const el = document.querySelector('#table-users');
		const boxes = [...el.querySelectorAll('tbody input.row-pick')];

		const sizes = boxes.map(box => {
			const r = box.getBoundingClientRect();
			return { width: r.width, height: r.height };
		});

		return JSON.stringify({
			rows: el.tBodies[0].rows.length,
			boxes: boxes.length,
			smallest: sizes.reduce((least, one) =>
				(one.width * one.height < least.width * least.height ? one : least),
				{ width: Infinity, height: Infinity }),
		});
	})()`, &table)

	if table.Rows < 2 {
		t.Fatalf("the accounts table holds %d row(s); this case needs the "+
			"administrator and somebody deletable", table.Rows)
	}

	if table.Boxes == 0 {
		t.Fatal("no account can be ticked, so several cannot be deleted at once")
	}

	// The measurement, which is the whole point of running this in a second
	// engine. 8px is well under any real checkbox and well over nought.
	if table.Smallest.Width < 8 || table.Smallest.Height < 8 {
		t.Errorf("a selection box renders %.0fx%.0f in Firefox - it is in the "+
			"document and not on the screen", table.Smallest.Width, table.Smallest.Height)
	}
}

// Nothing anywhere is in the document at no size.
//
// The general form of the bug above, asked across every screen an administrator
// can open. A control with no width is invisible and unclickable while being
// perfectly present to every query a test might make, which is exactly the shape
// of failure that survives a suite: it is not a wrong value, a missing element or
// a bad response. It is a number no one thought to measure.
//
// Buttons, fields and boxes only, and only ones that are meant to be on screen -
// a hidden panel is hidden on purpose, and an element with no size because an
// ancestor is display:none is not a fault.
func TestNoControlIsOnScreenAtNoSize(t *testing.T) {
	app := harness.Start(t)
	b := openBrowser(t)

	b.goTo(app.BaseURL())
	signIn(t, b)

	createAccount(t, b, "sven@example.com")

	b.goTo(app.BaseURL())
	b.settle()

	views := []string{
		"timesheets", "calendar", "overtime", "projects",
		"users", "roles", "report", "settings", "admin",
	}

	for _, view := range views {
		b.eval(fmt.Sprintf(`(() => {
			const tab = document.querySelector('.tab[data-view=%q]');
			if (tab) tab.click();
			return 1;
		})()`, view))

		b.settle()

		var tiny []struct {
			What   string  `json:"what"`
			Width  float64 `json:"width"`
			Height float64 `json:"height"`
		}

		b.evalJSON(`JSON.stringify((() => {
			const named = (el) => {
				const bits = [el.tagName.toLowerCase()];
				if (el.type) bits.push('[' + el.type + ']');
				if (el.id) bits.push('#' + el.id);
				if (el.name) bits.push('[name=' + el.name + ']');
				if (el.className && typeof el.className === 'string') {
					bits.push('.' + el.className.split(' ').filter(Boolean).join('.'));
				}
				return bits.join('');
			};

			const out = [];

			for (const el of document.querySelectorAll(
					'input, select, textarea, button')) {
				// offsetParent is null for anything inside a hidden view, which is
				// most of the page at any moment. Those are not on screen and are
				// not meant to be.
				if (!el.offsetParent && getComputedStyle(el).position !== 'fixed') continue;
				if (el.hidden || el.type === 'hidden') continue;

				const style = getComputedStyle(el);
				if (style.display === 'none' || style.visibility === 'hidden') continue;

				// Deliberately invisible controls, told apart from accidentally
				// invisible ones by the author having said so in the stylesheet.
				//
				// There are real ones here: the native date input behind each date
				// field is kept at 1px and clipped away, because it must stay in the
				// form and be able to open the platform's picker while the field
				// somebody actually reads is drawn beside it. That is the standard
				// way to hide a control without removing it, and it is not what this
				// case is looking for.
				//
				// Three statements of intent, any one of which excuses a control:
				// nought opacity, clipped to nothing, or not taking the pointer at
				// all. What is left is a control that expects to be used and cannot
				// be.
				if (Number(style.opacity) === 0) continue;
				if (style.clipPath && style.clipPath !== 'none') continue;
				if (style.pointerEvents === 'none') continue;

				const r = el.getBoundingClientRect();
				if (r.width >= 4 && r.height >= 4) continue;

				out.push({ what: named(el), width: r.width, height: r.height });
			}

			return out;
		})())`, &tiny)

		for _, control := range tiny {
			t.Errorf("on %s, %s renders %.0fx%.0f - it is on the screen and cannot "+
				"be seen or used", view, control.What, control.Width, control.Height)
		}
	}
}

// signIn takes the built-in administrator through everything that stands between
// a fresh instance and a usable screen.
//
// Through the API after the form, which is what the Chrome suite settled on too:
// the wizard and the initial password have their own cases there, and every other
// case wants them out of the way rather than exercised again.
func signIn(t *testing.T, b *browser) {
	t.Helper()

	b.eval(fmt.Sprintf(`(() => {
		const form = document.querySelector('#form-login');
		form.querySelector('input[name="email"]').value = %q;
		form.querySelector('input[name="password"]').value = %q;
		form.querySelector('button[type="submit"]').click();
		return 1;
	})()`, harness.AdminEmail, harness.AdminPassword))

	// Waited for rather than slept through. A fixed wait is a guess about the
	// machine running this, and on a loaded runner the guess was short - which
	// this reported as a sign-in that does not work in Firefox.
	b.waitFor(`document.querySelector('#login-screen').hidden === true`,
		"the sign-in screen never went away in Firefox")

	post(t, b, "/api/v1/settings/timezone", `{"timezone":"Europe/Berlin"}`, "PUT")
	post(t, b, "/api/v1/setup/complete", "null")
	post(t, b, "/api/v1/me/password", fmt.Sprintf(
		`{"currentPassword":%q,"newPassword":"a-longer-admin-password-1"}`,
		harness.AdminPassword), "PUT")

	// The wizard is an overlay across the page; the completion above is what
	// makes it stay away, and this is what takes it off the screen now.
	b.eval(`(() => {
		const wizard = document.querySelector('#setup-wizard');
		if (wizard) wizard.hidden = true;
		return 1;
	})()`)
}

// createAccount adds an ordinary account, so tables have a row that may be
// deleted.
func createAccount(t *testing.T, b *browser, email string) {
	t.Helper()

	post(t, b, "/api/v1/users", fmt.Sprintf(
		`{"name":"Sven","email":%q,"role":"user","password":"sven-password-1"}`, email))
}

// post sends one request from the page, so it carries the session and the token
// the browser holds.
func post(t *testing.T, b *browser, path, body string, method ...string) {
	t.Helper()

	verb := "POST"
	if len(method) > 0 {
		verb = method[0]
	}

	answer := b.evalString(fmt.Sprintf(`(async () => {
		const csrf = document.cookie.split(';').map(c => c.trim())
			.find(c => c.startsWith('gtr_csrf='))?.slice('gtr_csrf='.length) ?? '';

		const r = await fetch(%q, {
			method: %q,
			credentials: 'same-origin',
			headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
			body: JSON.stringify(%s),
		});

		return r.status + ' ' + (r.ok ? '' : (await r.text()).slice(0, 200));
	})()`, path, verb, body))

	if !strings.HasPrefix(answer, "20") {
		t.Fatalf("%s %s answered %s", verb, path, answer)
	}
}
