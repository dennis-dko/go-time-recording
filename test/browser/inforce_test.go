//go:build browser

package browser

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// What is in force keeps being said while the limits are being edited.
//
// The same split as the telemetry card beside it, found by asking of every
// beingEdited guard what sits behind it that is not the form's. Here it is the
// line reading "Currently in force: session 24 h, max/day 16 h, rate …" - which
// is the set of numbers somebody is weighing their own against while they type
// them.
//
// So the moment they start typing, the figures they are deciding against stop
// following the server. On this screen that includes what a restart has since
// put into force, which is the one time they change.
func TestWhatIsInForceIsStillSaidWhileTheLimitsAreEdited(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open the card", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-operational", chromedp.ByID))

	var before string

	p.run("what it says now", chromedp.Evaluate(
		`document.querySelector('#operational-effective').textContent.trim()`, &before))

	if before == "" {
		t.Fatal("the line is empty, so this case would pass whatever happens")
	}

	p.run("start filling it in", chromedp.Evaluate(`(() => {
		const form = document.querySelector('#form-operational');
		const field = form.elements.sessionLifetimeHours;

		field.value = '48';
		field.dispatchEvent(new Event('input', { bubbles: true }));

		return form.dataset.editing === 'yes';
	})()`, nil))

	var edited bool

	p.run("is it marked as being edited", chromedp.Evaluate(
		`document.querySelector('#form-operational').dataset.editing === 'yes'`, &edited))

	if !edited {
		t.Fatal("the form is not marked as being edited, so the early return this " +
			"case is about is never reached")
	}

	p.run("the limits in force change underneath", chromedp.Evaluate(`(() => {
		const real = window.fetch;

		window.fetch = async (input, init) => {
			const url = typeof input === 'string' ? input : input.url;
			const method = (init && init.method ? init.method : 'GET').toUpperCase();

			if (method === 'GET' && url.includes('/settings/operational')) {
				return new Response(JSON.stringify({ data: {
					configured: {},
					defaults: {},
					effective: {
						sessionLifetimeHours: 999,
						maxDailyHours: 17,
						rateLimit: 5,
						rateLimitWindowSeconds: 60,
						ldapSyncMaxDeleteRatio: 0.5,
					},
				} }), { status: 200, headers: { 'Content-Type': 'application/json' } });
			}

			return real(input, init);
		};

		return true;
	})()`, nil))

	p.run("ask again", chromedp.Evaluate(`loadOperational()`, nil, awaitPromise))

	var after string

	p.run("what it says after", chromedp.Evaluate(
		`document.querySelector('#operational-effective').textContent.trim()`, &after))

	if strings.Contains(after, "999") {
		return
	}

	t.Errorf("the line still reads %q after a fresh answer, because the form is "+
		"being edited. What is in force is not what was typed, and it is the "+
		"figure the typing is being weighed against", after)
}

// What is in force is written the way every other figure on screen is.
//
// The line built its hours by appending a literal "h" to the raw number, so a
// German reader saw "max./Tag 10.5 h" beside a timesheet saying "10,50 Std." -
// the unit and the separator both, on the one screen where the figures are
// being compared against what somebody is about to type.
//
// The share is written in full rather than to the two places an hour gets:
// 0.125 is a limit somebody can set, and "0,13" is one they did not.
func TestWhatIsInForceIsWrittenTheWayTheReaderWritesNumbers(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()
	p.chooseLanguage("de")

	p.run("open the card", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-operational", chromedp.ByID))

	p.run("answer with fractions", chromedp.Evaluate(`(() => {
		const real = window.fetch;

		window.fetch = async (input, init) => {
			const url = typeof input === 'string' ? input : input.url;
			const method = (init && init.method ? init.method : 'GET').toUpperCase();

			if (method === 'GET' && url.includes('/settings/operational')) {
				return new Response(JSON.stringify({ data: {
					configured: {},
					defaults: {},
					effective: {
						sessionLifetimeHours: 24,
						sessionIdleMinutes: 30,
						maxDailyHours: 10.5,
						rateLimit: 5,
						rateLimitWindowSeconds: 60,
						ldapSyncMaxDeleteRatio: 0.125,
					},
				} }), { status: 200, headers: { 'Content-Type': 'application/json' } });
			}

			return real(input, init);
		};

		return true;
	})()`, nil))

	p.run("ask again", chromedp.Evaluate(`loadOperational()`, nil, awaitPromise))

	var line, unit string

	p.run("read the line and the unit", chromedp.Evaluate(
		`document.querySelector('#operational-effective').textContent.trim()`, &line),
		chromedp.Evaluate(`t('unit.hours', 'h')`, &unit))

	if unit == "h" {
		t.Fatal("the German hour unit is \"h\" as well, so this case cannot tell the two apart")
	}

	for _, want := range []string{"24,00 " + unit, "10,50 " + unit, "Löschgrenze 0,125"} {
		if !strings.Contains(line, want) {
			t.Errorf("the line reads %q, which does not contain %q", line, want)
		}
	}

	for _, unwanted := range []string{" h,", "10.5", "0.125", "0,13"} {
		if strings.Contains(line, unwanted) {
			t.Errorf("the line reads %q, which still contains %q", line, unwanted)
		}
	}
}

// A person's working times are written in the users table the way every other
// hour figure is.
//
// The table put them through toFixed(1), which is the one form fmtNumber exists
// to replace: a German administrator read "7.8" for a target of seven and three
// quarter hours - the separator wrong and the figure rounded to one that was
// never set.
func TestTheUsersTableWritesWorkingTimesTheWayTheReaderWritesNumbers(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()
	p.chooseLanguage("de")

	p.run("open Users", p.click(`.tab[data-view="users"]`),
		chromedp.WaitVisible("#table-users", chromedp.ByID))

	p.run("answer with one account whose day is not a whole number", chromedp.Evaluate(`
		(async () => {
			const server = api;
			api = (path, options) => path === '/users'
				? Promise.resolve({ items: [{
					id: 4711, name: 'Sven', email: 'sven@example.com', role: 'user',
					isSystem: false, isExternal: false,
					dailyTargetHours: 7.75, maxDailyHours: 10.5,
				}] })
				: server(path, options);

			await loadUsers();

			return 1;
		})()`, nil, awaitPromise))

	p.waitForText("#table-users tbody", "sven@example.com")

	var row, unit string

	p.run("read the row and the unit", chromedp.Evaluate(
		`document.querySelector('#table-users tbody tr').textContent`, &row),
		chromedp.Evaluate(`t('unit.hours', 'h')`, &unit))

	for _, want := range []string{"7,75 " + unit, "10,50 " + unit} {
		if !strings.Contains(row, want) {
			t.Errorf("the row reads %q, which does not contain %q", row, want)
		}
	}

	for _, unwanted := range []string{"7.8", "10.5"} {
		if strings.Contains(row, unwanted) {
			t.Errorf("the row reads %q, which still contains %q", row, unwanted)
		}
	}
}
