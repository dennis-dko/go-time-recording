//go:build browser

package browser

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// streamOpen waits for the announcement stream's first connection, so that what
// a case dispatches afterwards is the browser coming back rather than arriving.
func (p *page) streamOpen() {
	p.t.Helper()

	deadline := time.Now().Add(waitPatience)

	var state int

	for time.Now().Before(deadline) {
		p.run("read the stream's state", chromedp.Evaluate(
			`announcements ? announcements.readyState : -1`, &state))

		if state == 1 {
			return
		}

		time.Sleep(100 * time.Millisecond)
	}

	p.t.Fatalf("the announcement stream is in state %d rather than open", state)
}

// A page that comes back to another build reloads, whether or not it heard the
// restart announced.
//
// The announcement lives in the memory of the process that made it. A page whose
// laptop slept through an update, or whose connection was down at the moment the
// restart was said, reconnected to the new process, was told nothing, and went
// on running the old version's script against the new one - which is what the
// comment above the reconnection calls the one thing not to do. The process that
// answers is made to report another version here, and the browser coming back is
// the event its own reconnection fires.
func TestAPageThatComesBackToAnotherVersionReloads(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()
	p.streamOpen()

	p.run("the process now answers as another build", chromedp.Evaluate(`(() => {
		window.__beforeReload = true;

		const server = api;
		api = async (path, options) => {
			const answer = await server(path, options);

			return String(path) === '/branding' ? { ...answer, version: 'v9.9.9' } : answer;
		};

		announcements.dispatchEvent(new Event('open'));

		return 1;
	})()`, nil))

	deadline := time.Now().Add(waitPatience)

	for {
		var gone bool

		p.run("wait for the reload", chromedp.Evaluate(`window.__beforeReload === undefined`, &gone))

		if gone {
			return
		}

		if time.Now().After(deadline) {
			t.Fatal("the page came back to another version and did not reload")
		}

		time.Sleep(250 * time.Millisecond)
	}
}

// A notice the server no longer stands by goes down when the page comes back.
//
// What is still worth saying is replayed to every connection, and a retraction
// is not kept: the hub forgets the update it takes back. So a page that was away
// when an installation was abandoned - a laptop shut while the download ran -
// came back to nothing that could take "a new version is being installed" down,
// and kept saying so for good.
func TestAPageThatComesBackTakesDownWhatNoLongerStands(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()
	p.streamOpen()

	p.run("an installation is announced", chromedp.Evaluate(
		`applyAnnouncement({ kind: 'update.installing', version: 'v9.9.9' })`, nil))

	if !p.visible("#update-banner") {
		t.Fatal("this case starts from the banner being up, and it is not")
	}

	p.run("the browser comes back to a server with nothing standing", chromedp.Evaluate(
		`announcements.dispatchEvent(new Event('open'))`, nil))

	p.waitGone("#update-banner")
}
