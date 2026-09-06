//go:build browser

package browser

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// What the process is serving keeps being said while somebody edits the form.
//
// fillTelemetryForm stops at beingEdited, which is right for the boxes: this runs
// after every save and after a language is chosen, and it used to replace what
// had been typed with the server's copy.
//
// Three things sit behind that early return and belong to none of it - the line
// saying what this process is actually serving, the running log level the viewer
// warns from, and that warning. The card one place up this same screen states the
// distinction and was fixed for it: "This card has two halves, and only one of
// them belongs to whoever is at the keyboard ... One touch of this form and the
// card went on saying 'connected via postgres' above five empty boxes for as long
// as the tab stayed open."
//
// Here the frozen half is the one the comment above it calls the thing somebody
// wants to copy: the metrics address, beside the log level and the exporter this
// process is running with. An administrator who types in the form and then
// restarts from the card below comes back to a line describing the process that
// has gone.
//
// The answer is stubbed rather than a restart driven: what this is about is
// whether a fresh answer reaches the line, not what makes the answer change.
func TestWhatIsRunningIsStillSaidWhileTheFormIsEdited(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open the card", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-telemetry", chromedp.ByID))

	var before string

	p.run("what it says now", chromedp.Evaluate(
		`document.querySelector('#telemetry-active').textContent.trim()`, &before))

	if before == "" {
		t.Fatal("the active line is empty, so this case would pass whatever happens")
	}

	// Typed into, the way a person marks a form as theirs: watchForEditing listens
	// for input on the form itself.
	p.run("start filling it in", chromedp.Evaluate(`(() => {
		const form = document.querySelector('#form-telemetry');
		const field = form.elements.tracerUrl;

		field.value = 'http://collector.example:4317';
		field.dispatchEvent(new Event('input', { bubbles: true }));

		return form.dataset.editing === 'yes';
	})()`, nil))

	var edited bool

	p.run("is it marked as being edited", chromedp.Evaluate(
		`document.querySelector('#form-telemetry').dataset.editing === 'yes'`, &edited))

	if !edited {
		t.Fatal("the form is not marked as being edited, so the early return this " +
			"case is about is never reached")
	}

	// A different process, as the server would describe it after a restart.
	p.run("the process changes underneath", chromedp.Evaluate(`(() => {
		const real = window.fetch;

		window.fetch = async (input, init) => {
			const url = typeof input === 'string' ? input : input.url;
			const method = (init && init.method ? init.method : 'GET').toUpperCase();

			if (method === 'GET' && url.includes('/settings/telemetry')) {
				return new Response(JSON.stringify({ data: {
					configured: {},
					active: {
						logLevel: 'NOTICE-FROM-THE-NEW-PROCESS',
						metricsServed: false,
						traceExporter: '',
					},
				} }), { status: 200, headers: { 'Content-Type': 'application/json' } });
			}

			return real(input, init);
		};

		return true;
	})()`, nil))

	p.run("ask again", chromedp.Evaluate(`loadTelemetry()`, nil, awaitPromise))

	var after string

	p.run("what it says after", chromedp.Evaluate(
		`document.querySelector('#telemetry-active').textContent.trim()`, &after))

	if strings.Contains(after, "NOTICE-FROM-THE-NEW-PROCESS") {
		return
	}

	t.Errorf("the line still reads %q after a fresh answer, because the form is "+
		"being edited. What the process is serving is not what was typed, and the "+
		"card beside this one already draws that line", after)
}
