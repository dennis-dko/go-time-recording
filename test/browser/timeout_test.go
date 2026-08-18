//go:build browser

package browser

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// A read that hangs is given up on; a write never is.
//
// The reason for the distinction is not visible at the call: aborting a request in
// the browser does not stop the server, which finishes the work either way. A
// timeout on a write would report a failure for something that succeeded, and the
// obvious response to that message is to do it again - which for an import means
// writing every row twice.
//
// Checked by intercepting fetch in the page rather than by waiting a minute for a
// real timeout: what has to hold is which calls carry an abort signal at all, and
// that is decided before a single millisecond passes. The paths are made up, so
// nothing is created or deleted by asking.
func TestOnlyReadsCarryATimeoutSignal(t *testing.T) {
	t.Parallel()

	p := open(t)

	var wiring struct {
		Read   bool `json:"read"`
		Write  bool `json:"write"`
		Caller bool `json:"caller"`
	}

	// Each call gets a path of its own, so anything else the page happens to fetch
	// while this runs cannot be mistaken for one of them.
	p.run("watch what api() hands to fetch", chromedp.Evaluate(`(async () => {
		const seen = new Map();
		const real = window.fetch;
		const mine = new AbortController();

		window.fetch = (url, init) => {
			seen.set(String(url), init);

			return real(url, init);
		};

		try {
			// Every one of these answers 404 or 401, which is all this needs: fetch
			// has already been called by then, with the object being examined.
			await api('/timeout-probe-read').catch(() => {});
			await api('/timeout-probe-write', { method: 'POST' }).catch(() => {});
			await api('/timeout-probe-caller', { signal: mine.signal }).catch(() => {});
		} finally {
			window.fetch = real;
		}

		const of = (path) => [...seen.entries()]
			.filter(([url]) => url.includes(path))
			.map(([, init]) => init)[0];

		return {
			read: Boolean(of('/timeout-probe-read')?.signal),
			write: Boolean(of('/timeout-probe-write')?.signal),
			// The caller's own signal, by identity: theirs, not one of ours wrapped
			// around it.
			caller: of('/timeout-probe-caller')?.signal === mine.signal,
		};
	})()`, &wiring, awaitPromise))

	if !wiring.Read {
		t.Error("a read carries no abort signal, so a request that never comes back " +
			"holds the in-flight counter above zero and the loading strip up for as " +
			"long as the connection lives")
	}

	if wiring.Write {
		t.Error("a write carries an abort signal; giving up on one reports a failure " +
			"for work the server has already finished, and the answer to that message " +
			"is to do it again")
	}

	if !wiring.Caller {
		t.Error("a caller's own signal was replaced, so whatever reason it had for " +
			"wanting to cancel no longer applies")
	}
}
