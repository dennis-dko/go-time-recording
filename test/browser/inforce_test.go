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
