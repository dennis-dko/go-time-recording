//go:build browser

package browser

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// The run the screen starts is bound to the preview it asked about.
//
// Pressing "Run synchronisation" previews, states the damage and on a yes starts
// the run - which asks the directory again and deletes whatever that second
// answer leaves out. The server now refuses a run whose candidates are not the
// ones confirmed, but only for a run that says which ones those were, so what
// matters here is what the screen sends: the ids it put in front of somebody,
// and an empty confirmation when the preview proposed nobody and no question
// was asked at all.
//
// The directory is played by the page's own api function, because this suite has
// none; what is being checked is the request the button produces, which the
// integration suite then holds the server to against a real directory.
func TestRunningTheSynchronisationSendsWhatWasConfirmed(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open the card", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#sync-run", chromedp.ByID))

	p.run("play a directory missing one account", chromedp.Evaluate(`(() => {
		const server = api;

		window.runs = [];
		window.proposed = [{ userId: 42, name: 'Dave', email: 'dave@example.com', timesheets: 3 }];

		api = (path, options) => {
			const report = (dryRun, candidates) => Promise.resolve({
				directoryUsers: 2, localExternal: 3, dryRun,
				candidates, deleted: dryRun ? [] : candidates, created: [],
			});

			if (path === '/settings/ldap/sync/preview') return report(true, window.proposed);

			if (path.startsWith('/settings/ldap/sync')) {
				window.runs.push(path);

				return report(false, window.proposed);
			}

			return server(path, options);
		};

		return true;
	})()`, nil))

	p.run("run, and say yes", p.click("#sync-run"),
		chromedp.WaitVisible(".confirm-overlay", chromedp.ByQuery),
		p.click(`.confirm-actions button.danger`))
	p.waitGone(".confirm-overlay")

	var first string

	p.run("what was sent", chromedp.Poll(`window.runs.length > 0 ? window.runs[0] : null`, &first))

	if !strings.HasSuffix(first, "?confirmed=42") {
		t.Errorf("the run confirmed for account 42 was sent as %q", first)
	}

	// And a preview that proposed nobody asks nothing, which is exactly when the
	// run most needs to say what it was agreed to be.
	var second string

	p.run("nobody proposed", chromedp.Evaluate(`window.proposed = []`, nil),
		p.click("#sync-run"),
		chromedp.Poll(`window.runs.length > 1 ? window.runs[1] : null`, &second))

	if !strings.HasSuffix(second, "?confirmed=") {
		t.Errorf("a run after a preview proposing nobody was sent as %q, "+
			"which does not bind it to deleting no one", second)
	}
}

// A run that fails leaves the card showing what is left, not the preview it
// started from.
//
// A run that stops part-way has still deleted what it reached, irreversibly.
// The screen was answered with the error alone and kept the preview drawn - two
// accounts that "would be deleted", one of them already gone, under a sentence
// saying the run had failed. It asks again now, so the list is what is left,
// and the refusal itself says how far the run got.
func TestAFailedSynchronisationShowsWhatIsLeft(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open the card", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#sync-run", chromedp.ByID))

	p.run("play a run that deletes one account and then fails", chromedp.Evaluate(`(() => {
		const server = api;

		window.remaining = [
			{ userId: 42, name: 'Dave', email: 'dave@example.com', timesheets: 3 },
			{ userId: 43, name: 'Erin', email: 'erin@example.com', timesheets: 5 },
		];

		api = (path, options) => {
			if (path === '/settings/ldap/sync/preview') {
				return Promise.resolve({
					directoryUsers: 2, localExternal: 4, dryRun: true,
					candidates: window.remaining, deleted: [], created: [],
				});
			}

			if (path.startsWith('/settings/ldap/sync')) {
				window.remaining = window.remaining.slice(1);

				return Promise.reject(new Error('the run stopped part-way'));
			}

			return server(path, options);
		};

		return true;
	})()`, nil))

	p.run("run, and say yes", p.click("#sync-run"),
		chromedp.WaitVisible(".confirm-overlay", chromedp.ByQuery),
		p.click(`.confirm-actions button.danger`))
	p.waitGone(".confirm-overlay")

	p.run("the list settles", chromedp.Poll(`!document.querySelector('#table-sync tbody')
		.textContent.includes('dave@example.com')`, nil, chromedp.WithPollingTimeout(10*time.Second)))

	if listed := p.text("#table-sync tbody"); !strings.Contains(listed, "erin@example.com") {
		t.Errorf("after the failed run the card lists %q, want the account that is left", listed)
	}
}
