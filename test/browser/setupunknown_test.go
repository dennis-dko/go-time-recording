//go:build browser

package browser

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// A wizard step this page does not know must not take the interface down with it.
//
// loadSetup states the policy itself, in the comment above its own catch: "Not
// fatal: an installation is usable without the wizard, and failing here would
// block the interface over a hint." The try it wrote that over covers the fetch.
// renderSetup() is called four lines below it, outside that try - and renderSetup
// looks a step up in SETUP_STEPS twice. The list is guarded, with
// SETUP_STEPS[s.id]?.title() ?? s.id, because whoever wrote it knew an id might be
// unknown. The detail pane three lines down is not, so it throws on the same id
// the line above it handled.
//
// loadSetup is awaited early inside refreshAll - before every other loader, on
// purpose, because the server blocks the rest of the API while the initial
// password stands. So the throw takes refreshAll with it and nothing below it
// runs: the wizard is a hint, and the hint empties the screen.
//
// Reached by a server that knows a step this page does not, which normally cannot
// last: settleAfterRestart reloads the tab when the build changed. It lasts when
// the new build arrives without this tab being told - a compose pull, a container
// rollout, an update applied from somebody else's browser - and those are
// deployment shapes deploy/OPERATIONS.md documents.
func TestAnUnknownSetupStepDoesNotEmptyTheScreen(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	// A newer server, answering with a step this page has no definition for.
	p.run("answer with a step from a later version", chromedp.Evaluate(`(() => {
		const real = window.fetch;

		window.fetch = async (input, init) => {
			const url = typeof input === 'string' ? input : input.url;
			const method = (init && init.method ? init.method : 'GET').toUpperCase();

			if (method === 'GET' && url.includes('/api/v1/setup')) {
				return new Response(JSON.stringify({ data: {
					completed: false,
					steps: [{ id: 'somethingLaterVersionsAdded', required: true, done: false }],
				} }), { status: 200, headers: { 'Content-Type': 'application/json' } });
			}

			return real(input, init);
		};

		return true;
	})()`, nil))

	var outcome string

	p.run("load everything again", chromedp.Evaluate(`(async () => {
		try {
			await refreshAll();

			return 'loaded';
		} catch (err) {
			return 'threw: ' + err.message;
		}
	})()`, &outcome, awaitPromise))

	if outcome != "loaded" {
		t.Errorf("refreshAll %s. A step id this page does not know reached "+
			"renderSetup, which guards the same lookup in the list beside it and "+
			"not in the detail pane - and loadSetup runs before every other loader, "+
			"so the whole screen goes with it", outcome)
	}

	// And the screen is actually populated, not merely free of an exception.
	var state string

	// The accounts table, because this administrator always has a row in it -
	// itself - whereas the project lists are empty until somebody makes a project.
	p.run("the interface is there", chromedp.Evaluate(`(() => {
		const loaded = document.documentElement.dataset.loaded;
		const rows = document.querySelectorAll('#table-users tbody tr').length;

		return loaded + '/' + rows;
	})()`, &state))

	if !strings.HasPrefix(state, "yes/") || strings.HasSuffix(state, "/0") {
		t.Errorf("refreshAll returned without throwing but the screen is not "+
			"loaded (data-loaded/account rows: %s); a wizard the page cannot "+
			"render still cost the interface", state)
	}

	if strings.Contains(outcome, "threw") {
		t.Log("the wizard is a hint, and the hint emptied the screen")
	}
}
