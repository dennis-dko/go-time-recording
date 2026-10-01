//go:build browser

package browser

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/dennis-dko/go-time-recording/internal/support/spreadsheet"
)

// A booking pressed twice is booked once.
//
// The form sends its write and resets itself when the answer comes, and nothing
// stood between the two: a double click - or Enter pressed twice - sent the
// second request before the first had been answered, and the same hours were
// recorded twice, counted twice in the balance and shown as two rows the reader
// had to notice and delete. Measured: two entries, created the same second.
func TestABookingPressedTwiceIsBookedOnce(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.run("open the entries view", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID))

	p.run("fill in a booking",
		chromedp.SendKeys(`#form-timesheet input[name="durationHours"]`, "1.5", chromedp.ByQuery),
		chromedp.SendKeys(`#form-timesheet input[name="description"]`, "pressed twice",
			chromedp.ByQuery))

	p.run("press it twice", chromedp.Evaluate(`(() => {
		const button = document.querySelector('#form-timesheet button[type="submit"]');
		button.click();
		button.click();
		return true;
	})()`, nil))

	// Done once the form has been reset by the answer and nothing is in flight -
	// the page's own count of requests - so a second request, had one been sent,
	// has been answered too.
	done := false

	for deadline := time.Now().Add(waitPatience); !done && time.Now().Before(deadline); {
		p.run("see whether the booking is answered", chromedp.Evaluate(`progress.inFlight === 0
			&& document.querySelector('#form-timesheet input[name="durationHours"]').value === ''`, &done))

		if !done {
			time.Sleep(50 * time.Millisecond)
		}
	}

	if !done {
		t.Fatal("the booking was never answered")
	}

	var count int

	p.run("ask the server how many there are", chromedp.Evaluate(`(async () => {
		const res = await fetch('/api/v1/timesheets', { credentials: 'same-origin' });
		const body = await res.json();
		return (body.data.items ?? []).filter((e) => e.description === 'pressed twice').length;
	})()`, &count, awaitPromise))

	if count != 1 {
		t.Errorf("one booking pressed twice was recorded %d times", count)
	}
}

// An import pressed twice imports once.
//
// The same hole as a booking, with every row of the file behind it: the import
// asks fetch directly, with the file as its body, and a second press sent the
// file again before the first had been answered.
func TestAnImportPressedTwiceImportsOnce(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	book, err := spreadsheet.Write([]spreadsheet.Row{
		{Date: time.Now(), Hours: 2, Description: "imported once"},
	})
	if err != nil {
		t.Fatalf("building the workbook: %v", err)
	}

	path := filepath.Join(t.TempDir(), "once.xlsx")
	if err := os.WriteFile(path, book, 0o600); err != nil {
		t.Fatalf("writing the workbook: %v", err)
	}

	p.run("choose the file",
		chromedp.Click(`.tab[data-view="timesheets"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#workbook-card", chromedp.ByID),
		chromedp.SetUploadFiles("#wb-file", []string{path}, chromedp.ByQuery))

	p.waitShown("#wb-preview")

	p.run("check it", p.click("#wb-preview"),
		chromedp.WaitVisible("#wb-import", chromedp.ByID))

	p.run("import it twice", chromedp.Evaluate(`(() => {
		const button = document.querySelector('#wb-import');
		button.click();
		button.click();
		return true;
	})()`, nil))

	// Done once the card has been reset by the answer and nothing is in flight.
	done := false

	for deadline := time.Now().Add(waitPatience); !done && time.Now().Before(deadline); {
		p.run("see whether the import is answered", chromedp.Evaluate(
			`progress.inFlight === 0 && document.querySelector('#wb-import').hidden`, &done))

		if !done {
			time.Sleep(50 * time.Millisecond)
		}
	}

	if !done {
		t.Fatal("the import was never answered")
	}

	var count int

	p.run("ask the server how many there are", chromedp.Evaluate(`(async () => {
		const res = await fetch('/api/v1/timesheets', { credentials: 'same-origin' });
		const body = await res.json();
		return (body.data.items ?? []).filter((e) => e.description === 'imported once').length;
	})()`, &count, awaitPromise))

	if count != 1 {
		t.Errorf("one import pressed twice wrote its row %d times", count)
	}
}
