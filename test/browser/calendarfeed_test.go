//go:build browser

package browser

import (
	"strconv"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/dennis-dko/go-time-recording/internal/support/qrcode"
)

// A calendar address is made on the account screen and shown there once, beside a
// QR code of exactly that address for the phone a calendar is usually on: a
// calendar reads the booked day at it without a session, it leaves the screen
// with the screen - the code with it - and making a new one, after saying what
// that does, takes the old one back, as removing it does.
func TestACalendarAddressIsShownOnceAndTakenBack(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.awaitJSON(`api('/timesheets', {
		method: 'POST', body: JSON.stringify({ date: todayISO(), durationHours: 3 }),
	}).then(() => true)`, nil)

	p.run("open my account", p.click(`.tab[data-view="settings"]`),
		chromedp.WaitVisible("#feed-card", chromedp.ByID))

	p.waitForText("#feed-state", "No address yet.")

	p.run("make an address", p.click("#feed-create"),
		chromedp.WaitVisible("#feed-address", chromedp.ByID))

	first := strings.TrimSpace(p.text("#feed-address-value"))
	if !strings.Contains(first, "/api/v1/calendar-feed/gtrcal_") || !strings.HasSuffix(first, ".ics") {
		t.Fatalf("the address on screen is %q, want a feed's", first)
	}

	code := func() string {
		t.Helper()

		var src string

		p.run("read the code", chromedp.Evaluate(`document.querySelector('#feed-qr')?.getAttribute('src') ?? ''`, &src))

		return src
	}

	if want, err := qrcode.SVGDataURI(first); err != nil || code() != want || !p.visible("#feed-qr") {
		t.Errorf("the code beside the address is not the address on screen (src %.40q, shown %v)",
			code(), p.visible("#feed-qr"))
	}

	// As a calendar program reads it: the address and no cookie.
	read := func(address string) (status int, body string) {
		t.Helper()

		var answer struct {
			Status int    `json:"status"`
			Body   string `json:"body"`
		}

		p.awaitJSON(`fetch(`+strconv.Quote(address)+`, { credentials: 'omit' })
			.then(async (r) => ({ status: r.status, body: await r.text() }))`, &answer)

		return answer.Status, answer.Body
	}

	if status, body := read(first); status != 200 || !strings.Contains(body, "SUMMARY:3.00 h booked") {
		t.Errorf("the address answered %d without the booked day:\n%s", status, body)
	}

	p.waitForText("#feed-state", "gtrcal_")

	if label := p.text("#feed-create"); label != "New address" {
		t.Errorf("with an address the button says %q, want New address", label)
	}

	p.run("go somewhere else and back", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID),
		p.click(`.tab[data-view="settings"]`),
		chromedp.WaitVisible("#feed-card", chromedp.ByID))

	if p.visible("#feed-address") || strings.TrimSpace(p.text("#feed-address-value")) != "" {
		t.Errorf("the address outlived the screen it was shown on")
	}

	if left := code(); left != "" {
		t.Errorf("the address's code outlived the screen it was shown on (%.40q)", left)
	}

	p.run("make a new address", p.click("#feed-create"),
		chromedp.WaitVisible(".confirm-overlay", chromedp.ByQuery),
		p.click(`.confirm-actions button.danger`),
		chromedp.WaitVisible("#feed-address", chromedp.ByID))

	second := strings.TrimSpace(p.text("#feed-address-value"))

	if second == first || second == "" {
		t.Fatalf("the new address is %q, want another than %q", second, first)
	}

	if status, _ := read(first); status != 404 {
		t.Errorf("the address a new one replaced answered %d, want 404", status)
	}

	p.run("remove it", p.click("#feed-remove"),
		chromedp.WaitVisible(".confirm-overlay", chromedp.ByQuery),
		p.click(`.confirm-actions button.danger`))

	p.waitForText("#toast", "Address removed")
	p.waitForText("#feed-state", "No address yet.")

	if status, _ := read(second); status != 404 {
		t.Errorf("the address of a removed feed answered %d, want 404", status)
	}

	if p.visible("#feed-address") || p.visible("#feed-remove") {
		t.Errorf("after removing it the card still shows an address or a way to remove one")
	}
}

// A sign-out takes the calendar card back to knowing nothing: not the address, and
// not what the card said about the last account's - the start of its key, when it
// was made and last read - for whoever signs in next to find before their own is
// loaded, or to find for good when their role shows them no card at all.
func TestASignOutTakesTheCalendarCardWithIt(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.run("make an address", p.click(`.tab[data-view="settings"]`),
		chromedp.WaitVisible("#feed-card", chromedp.ByID),
		p.click("#feed-create"),
		chromedp.WaitVisible("#feed-address", chromedp.ByID))

	p.waitForText("#feed-state", "gtrcal_")

	p.run("sign out", p.click("#logout"), chromedp.WaitVisible("#form-login", chromedp.ByID))

	var left []string

	p.run("read what the next account would find", chromedp.Evaluate(`(() => {
		const left = [];

		for (const text of ['#feed-state', '#feed-address-value']) {
			if (document.querySelector(text).textContent.trim()) left.push(text + ' says ' + document.querySelector(text).textContent);
		}

		if (!document.querySelector('#feed-remove').hidden) left.push('#feed-remove is offered');
		if (document.querySelector('#feed-create').dataset.i18n !== 'feed.create') left.push('#feed-create offers a new address');

		return left;
	})()`, &left))

	for _, what := range left {
		t.Errorf("after the sign-out %s", what)
	}
}
