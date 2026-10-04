//go:build browser

package browser

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// A button turned off while its request runs gives the focus back afterwards.
//
// A focused button that becomes disabled loses the focus to the page. Somebody
// who pressed "Check for updates" or an export from the keyboard was afterwards
// nowhere, and the next Tab began again at the top of the page. The installer
// had the same fault, found first.

// focusAfterPressing presses a button from the keyboard and answers where the
// focus is once the button is back in service.
func (p *page) focusAfterPressing(button string) string {
	p.t.Helper()

	p.run("press "+button+" from the keyboard",
		chromedp.Focus(button, chromedp.ByQuery),
		chromedp.KeyEvent(kb.Enter))

	var back bool

	deadline := time.Now().Add(waitPatience)

	for time.Now().Before(deadline) {
		p.run("is "+button+" back", chromedp.Evaluate(
			`!document.querySelector('`+button+`').disabled`, &back))

		if back {
			break
		}

		time.Sleep(100 * time.Millisecond)
	}

	if !back {
		p.t.Fatalf("%s never came back into service", button)
	}

	var focused string

	p.run("where is the focus", chromedp.Evaluate(
		`document.activeElement ? '#' + document.activeElement.id : ''`, &focused))

	return focused
}

func TestCheckingForUpdatesGivesTheFocusBack(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()
	p.settleReleaseWatch()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#update-check", chromedp.ByID))

	// An answer that takes a moment, as a feed on the other side of the world does.
	p.run("answer the check slowly", chromedp.Evaluate(`
		(() => {
			const server = api;
			api = (path, options) => path === '/settings/update/check'
				? new Promise((resolve) => setTimeout(() => resolve({ running: 'v1.0.0',
					latest: 'v1.0.0', newer: false, enabled: true, comparable: true,
					installable: true, available: true, restartable: true }), 300))
				: server(path, options);
			return 1;
		})()`, nil))

	if focused := p.focusAfterPressing("#update-check"); focused != "#update-check" {
		t.Errorf("after the check the focus is on %q rather than on the button that was "+
			"pressed", focused)
	}
}

func TestExportingADocumentGivesTheFocusBack(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	// The result, and the button under it, appear once a balance is worked out.
	p.run("work out the balance", p.click(`.tab[data-view="overtime"]`),
		chromedp.WaitVisible("#form-overtime", chromedp.ByID),
		p.click(`#form-overtime button[type="submit"]`),
		chromedp.WaitVisible("#overtime-pdf", chromedp.ByID))

	p.run("download slowly", chromedp.Evaluate(`
		(() => {
			downloadDocument = () => new Promise((resolve) => setTimeout(resolve, 300));
			return 1;
		})()`, nil))

	if focused := p.focusAfterPressing("#overtime-pdf"); focused != "#overtime-pdf" {
		t.Errorf("after the export the focus is on %q rather than on the button that was "+
			"pressed", focused)
	}
}
