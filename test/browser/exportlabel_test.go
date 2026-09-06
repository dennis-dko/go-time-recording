//go:build browser

package browser

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// The export button goes back to saying what it does, in either language.
//
// While a document is being built the button says "Preparing …", written straight
// into it with textContent while the element still declares data-i18n as
// action.exportPdf. That is the shape swapTheLabel exists for, and here it has a
// second edge: applyLanguage takes its copy of the English source the first time
// it translates an element, so a language change during an export copies
// "Preparing …" as the English of a button that exports.
//
// The copy is what makes it stick. The export finishes and puts the old label
// back, and everything looks right - until the page is read in English again,
// where a button that is doing nothing offers to keep preparing, for the rest of
// the session.
//
// The export is held open on purpose rather than raced: this needs the language
// to change while the button is saying the other thing, and a case that depends
// on catching a two-second window is a case that fails on somebody else's
// machine.
func TestTheExportButtonDoesNotKeepSayingPreparing(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.run("hold the document open", chromedp.Evaluate(`(() => {
		const real = window.fetch;
		window.__release = null;

		window.fetch = (input, init) => {
			const url = typeof input === 'string' ? input : input.url;

			if (url.includes('/exports/document')) {
				return new Promise((resolve) => {
					window.__release = () => resolve(real(input, init));
				});
			}

			return real(input, init);
		};

		return true;
	})()`, nil))

	// The card the button lives on is hidden until something has been evaluated.
	p.run("evaluate the balance", p.click(`.tab[data-view="overtime"]`),
		chromedp.WaitVisible("#form-overtime", chromedp.ByID),
		p.click(`#form-overtime button[type="submit"]`),
		chromedp.WaitVisible("#overtime-result", chromedp.ByID))

	p.run("ask for the document",
		chromedp.WaitVisible("#overtime-pdf", chromedp.ByID),
		p.click("#overtime-pdf"))

	// Waited on the request being in flight rather than on the words, so the case
	// says which half went wrong if it stalls.
	deadline := time.Now().Add(waitPatience)

	var held bool

	for time.Now().Before(deadline) {
		p.run("is it held", chromedp.Evaluate(
			`typeof window.__release === 'function'`, &held))

		if held {
			break
		}

		time.Sleep(100 * time.Millisecond)
	}

	if !held {
		t.Fatal("the export never reached the document request, so there is no " +
			"window for the language to change in")
	}

	var saying string

	p.run("what the button says while it works", chromedp.Evaluate(
		`document.querySelector('#overtime-pdf').textContent.trim()`, &saying))

	if saying != "Preparing …" {
		t.Fatalf("the button reads %q while exporting rather than \"Preparing …\"; "+
			"this case is not measuring what it thinks it is", saying)
	}

	p.chooseLanguage("de")

	p.run("let the document through", chromedp.Evaluate(`window.__release()`, nil))

	p.chooseLanguage("en")

	// Given a moment for the export to have finished putting its label back.
	time.Sleep(2 * time.Second)

	var afterwards string

	p.run("what the button says now", chromedp.Evaluate(
		`document.querySelector('#overtime-pdf').textContent.trim()`, &afterwards))

	if afterwards == "Export as PDF" {
		return
	}

	t.Errorf("back in English the export button reads %q. applyLanguage copied "+
		"the English source while the button was saying it was preparing, so the "+
		"copy it restores is that", afterwards)
}
