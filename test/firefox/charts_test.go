//go:build firefox

package firefox

import (
	"fmt"
	"testing"

	"github.com/dennis-dko/go-time-recording/test/harness"
)

// Nothing a chart says runs past the edge of its drawing on a telephone, in
// Firefox.
//
// A chart is drawn as wide as its box, and the bars' figures stood flush against
// the right-hand edge of the drawing. Blink measures the last glyph of "2.75 h" as
// ending on that edge, so the Chrome case holding the same rule passed; Gecko
// measures it a pixel past it, where the drawing clips. On a wide screen the
// drawing sits inside a margin and nothing showed. At 390 pixels the box is the
// drawing.
func TestNoChartTextRunsPastItsDrawingInFirefox(t *testing.T) {
	app := harness.Start(t)
	b := openBrowser(t)

	b.goTo(app.BaseURL())
	signIn(t, b)
	createAccount(t, b, "sven@example.com")

	// The built-in administrator records no time, so the chart is Sven's.
	b.eval(`(() => { document.querySelector('#logout').click(); return 1; })()`)
	b.waitFor(`document.querySelector('#login-screen').hidden === false`,
		"the sign-in screen never came back in Firefox")

	b.eval(`(() => {
		const form = document.querySelector('#form-login');
		form.querySelector('input[name="email"]').value = 'sven@example.com';
		form.querySelector('input[name="password"]').value = 'sven-password-1';
		form.querySelector('button[type="submit"]').click();
		return 1;
	})()`)
	b.waitFor(`document.querySelector('#login-screen').hidden === true`,
		"Sven never got in, in Firefox")
	b.settle()

	// Today in the browser's own zone, which is what the evaluation's default
	// period ends on.
	today := b.evalString(`(() => {
		const now = new Date();
		return now.getFullYear() + '-' + String(now.getMonth() + 1).padStart(2, '0') + '-'
			+ String(now.getDate()).padStart(2, '0');
	})()`)

	post(t, b, "/api/v1/timesheets", fmt.Sprintf(`{"date":%q,"durationHours":2.75}`, today))

	b.call("browsingContext.setViewport", map[string]any{
		"context":  b.firstContext(),
		"viewport": map[string]any{"width": 390, "height": 844},
	})

	b.eval(`(() => {
		document.querySelector('#tour-end')?.click();
		switchView('report');
		document.querySelector('#form-report button[type="submit"]').click();
		return 1;
	})()`)
	b.waitFor(`!document.querySelector('#report-result').hidden`, "the evaluation never arrived in Firefox")
	b.settle()

	for _, kind := range []string{"bars", "columns", "pie"} {
		b.eval(fmt.Sprintf(`(() => {
			document.querySelector('#report-chart-switch button[data-chart=%q]').click();
			return 1;
		})()`, kind))

		past := b.evalString(`(() => {
			const box = document.querySelector('#report-chart');
			const seen = box.getBoundingClientRect();

			return [...box.querySelectorAll('.chart-label, .chart-value')].map((text) => {
				const drawn = text.getBoundingClientRect();
				const out = drawn.height > 0 && (drawn.left < seen.left - 0.5 || drawn.right > seen.right + 0.5);

				return out ? text.textContent + ' (' + drawn.left.toFixed(1) + '..' + drawn.right.toFixed(1) + ')' : '';
			}).filter(Boolean).join('; ');
		})()`)

		if past != "" {
			box := b.evalString(`(() => {
				const seen = document.querySelector('#report-chart').getBoundingClientRect();
				return seen.left.toFixed(1) + '..' + seen.right.toFixed(1);
			})()`)

			t.Errorf("drawn as %s at 390px, the chart runs past its drawing (%s) in Firefox: %s", kind, box, past)
		}
	}
}
