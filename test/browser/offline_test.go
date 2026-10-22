//go:build browser

package browser

import (
	"context"
	"strings"
	"testing"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// connection takes the page's network away or gives it back, the way a train
// tunnel does: for the page, the service worker and navigator.onLine alike.
func (p *page) connection(on bool) {
	p.t.Helper()

	conditions := []*network.Conditions{}
	if !on {
		conditions = append(conditions, &network.Conditions{Offline: true, DownloadThroughput: -1, UploadThroughput: -1})
	}

	p.run("take the connection away or give it back", network.Enable(),
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := network.EmulateNetworkConditionsByRule(conditions).WithEmulateOfflineServiceWorker(!on).Do(ctx)

			return err
		}),
		network.OverrideNetworkState(!on, 0, -1, -1))
}

// A booking made without a connection waits on the device and says so; the
// installed application opens without one from what the service worker kept and
// says why nobody is signed in; and once the connection is back the page carries
// on by itself and sends what waited, which is then a booking like any other.
func TestABookingMadeWithoutAConnectionIsSentOnceItIsBack(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	var kept bool

	p.awaitJSON(`(async () => {
		await navigator.serviceWorker.ready;
		for (let i = 0; i < 100; i += 1) {
			if (await caches.match('/app.js')) return true;
			await new Promise((resolve) => setTimeout(resolve, 100));
		}
		return false;
	})()`, &kept)

	if !kept {
		t.Fatal("the service worker never kept the shell")
	}

	p.run("open the entries", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID))

	p.connection(false)
	p.waitForText("#outbox-text", "No connection to the installation")

	p.run("book without a connection",
		chromedp.SetValue(`#form-timesheet input[name="durationHours"]`, "1.25", chromedp.ByQuery),
		chromedp.SetValue(`#form-timesheet input[name="description"]`, "on the train", chromedp.ByQuery),
		p.click("#timesheet-submit"))
	p.waitForText("#outbox-text", "waiting on this device: 1")

	if n := p.app.Rows(t, "timesheets", "description = ?", "on the train"); n != 0 {
		t.Fatalf("%d booking(s) reached the server without a connection", n)
	}

	// A reload with no connection: the page comes from what the service worker
	// kept, and the sign-in screen says why it is there.
	p.run("open it again without a connection", chromedp.Reload())

	p.run("wait for the offline sign-in", chromedp.WaitVisible("#login-offline", chromedp.ByID))

	// The connection back: the page starts again by itself and sends what waited.
	p.connection(true)
	p.waitGone("#login-screen")
	p.settled()

	p.run("open the entries", p.click(`.tab[data-view="timesheets"]`))
	p.waitForText("#table-timesheets tbody", "on the train")

	if p.visible("#outbox-banner") {
		t.Errorf("the outbox banner still says %q after everything was sent", p.text("#outbox-banner"))
	}

	// Once, and only once. The text in the list was there just the same when the
	// page that sent it was replaced before it could say so and the next one sent
	// it again - two entries for one booking, which only a count tells apart.
	if n := p.app.Rows(t, "timesheets", "description = ?", "on the train"); n != 1 {
		t.Errorf("the booking made without a connection is on the server %d times, want once", n)
	}
}

// A booking the installation refuses once it can be asked again stays with the
// reason, and correcting it puts it back into the form and out of the outbox.
func TestARefusedBookingMadeWithoutAConnectionWaitsToBeCorrected(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	var waiting bool

	p.awaitJSON(`(async () => {
		const closed = await api('/projects', {
			method: 'POST', body: JSON.stringify({ name: 'Closed', startDate: '2026-01-01' }),
		});
		await api('/projects/' + closed.id, { method: 'PUT', body: JSON.stringify({ status: 'completed' }) });
		keepForLater({ projectId: closed.id, date: todayISO(), durationHours: 2, description: 'too late' });
		await sendOutbox();
		return readOutbox().length === 1 && Boolean(readOutbox()[0].refused);
	})()`, &waiting)

	if !waiting {
		t.Fatal("the refused booking did not stay in the outbox with its reason")
	}

	p.waitForText("#outbox-refused", "too")

	if text := p.text("#outbox-refused"); !strings.Contains(text, "Closed") {
		t.Errorf("the refused booking reads %q, want the installation's reason naming the project", text)
	}

	p.run("correct it", p.click("#outbox-refused button"))
	p.waitGone("#outbox-banner")

	if got := p.value(`#form-timesheet input[name="description"]`); got != "too late" {
		t.Errorf("the form holds %q, want the refused booking's description", got)
	}
}

// A booking a page began sending and did not live to clear - replaced, closed,
// its battery gone - is asked about rather than sent again: found on the server,
// it leaves the outbox, and the installation holds it once.
func TestABookingAPageDidNotLiveToClearIsNotSentTwice(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	var left int

	p.awaitJSON(`(async () => {
		const body = { date: todayISO(), durationHours: 3, description: 'half sent' };
		const keptAt = new Date(Date.now() - 60000).toISOString();
		await api('/timesheets', { method: 'POST', body: JSON.stringify(body) });
		localStorage.setItem(outboxKey(), JSON.stringify([{ id: 'a', body, keptAt, sending: true }]));
		await sendOutbox();
		return readOutbox().length;
	})()`, &left)

	if left != 0 {
		t.Errorf("%d booking(s) still wait after the round, want the one that had arrived gone", left)
	}

	if n := p.app.Rows(t, "timesheets", "description = ?", "half sent"); n != 1 {
		t.Errorf("the booking is on the server %d times, want once", n)
	}
}
