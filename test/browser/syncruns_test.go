//go:build browser

package browser

import (
	"slices"
	"testing"

	"github.com/chromedp/chromedp"
)

// The directory card lists what recent runs changed, and names nobody.
//
// A run deletes accounts with the hours on them, and its only record was the
// log. The list says when each run that removed or added an account ran,
// whether somebody confirmed it against a preview, and how much it took or
// brought - in the reader's zone and words, as every moment and label here is.
func TestTheDirectoryCardListsWhatRecentRunsChanged(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#sync-card", chromedp.ByID))

	// A new installation has had no run, and says so rather than showing an
	// empty table.
	p.waitEvaluates("the list says there has been no run", `String(
		document.querySelector('#table-sync-runs tbody')?.textContent.trim() ===
			t('sync.noRuns', 'No run has removed or added an account yet.'))`, "true")

	p.run("answer the runs", chromedp.Evaluate(`(() => {
		const real = window.fetch;
		window.fetch = async (input, init) => {
			const url = typeof input === 'string' ? input : input.url;

			if (url.includes('/settings/ldap/sync/runs')) {
				return new Response(JSON.stringify({ data: { items: [
					{ ranAt: '2026-10-09T03:00:00Z', confirmed: false, deleted: 2, entriesDeleted: 37, created: 1 },
					{ ranAt: '2026-10-08T14:30:00Z', confirmed: true, deleted: 1, entriesDeleted: 5, created: 0 },
				], totalCount: 2 } }), { status: 200, headers: { 'Content-Type': 'application/json' } });
			}

			return real(input, init);
		};

		return 1;
	})()`, nil))

	p.run("load the screen again", chromedp.Evaluate(`void refreshAll()`, nil))

	p.waitEvaluates("the runs are listed",
		`String(document.querySelectorAll('#table-sync-runs tbody tr').length)`, "2")
	p.atRest()

	var shown, want [][]string

	p.evalJSON(`JSON.stringify([...document.querySelectorAll('#table-sync-runs tbody tr')]
		.map((row) => [...row.cells].map((cell) => cell.textContent.trim())))`, &shown)

	p.evalJSON(`JSON.stringify([
		[fmtMoment('2026-10-09T03:00:00Z'), t('sync.unattended', 'Schedule or script'), '2', '37', '1'],
		[fmtMoment('2026-10-08T14:30:00Z'), t('sync.byPerson', 'Confirmed against a preview'), '1', '5', '0'],
	])`, &want)

	if !slices.EqualFunc(shown, want, slices.Equal[[]string]) {
		t.Errorf("the card lists the runs as\n\t%q\nwant\n\t%q", shown, want)
	}
}
