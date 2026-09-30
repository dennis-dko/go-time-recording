//go:build browser

package browser

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// A failure the server could not put into words keeps its words on screen.
//
// For an internal failure the server answers with a sentence for the reader, and
// with the original text and a reference for whoever is going to fix it - and the
// German sentence says the technical details are underneath. Two error toasts put
// them underneath; nine passed only the sentence, the install among them, so an
// update refused over its checksum said "Die technischen Details stehen darunter"
// with nothing under it, and no reference to find the log line by.
//
// The install is the case here because it is the one an administrator acts on:
// whether to try again, free some disk or report a broken release depends
// entirely on the words that were dropped.
func TestAFailedInstallShowsWhatWentWrong(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#update-card", chromedp.ByID))

	// A build called "dev" is offered no button, so it is shown here; what is
	// under test is what the screen does with the answer, not the offer.
	p.run("answer the install with an internal failure", chromedp.Evaluate(`
		(() => {
			const server = api;
			api = (path, options) => path === '/settings/update' && options?.method === 'POST'
				? Promise.reject(Object.assign(new Error(t('err.internal', 'Something went wrong.')), {
					status: 500,
					refusal: {
						code: 'internal',
						message: 'the request could not be completed',
						detail: 'the download does not match the published checksum',
						ref: 'A7F3C2',
					},
				}))
				: server(path, options);
			$('#update-now').hidden = false;
			return 1;
		})()`, nil))

	p.run("press it", p.click("#update-now"),
		chromedp.WaitVisible(".confirm-overlay", chromedp.ByQuery))
	p.run("confirm", p.click(`.confirm-actions button.danger`))

	p.waitShown(".toast-note.error")

	detail := p.text(".toast-note.error .refusal-text")

	for _, want := range []string{"does not match the published checksum", "A7F3C2"} {
		if !strings.Contains(detail, want) {
			t.Errorf("the failed install's toast does not carry %q; under it: %q (toast: %q)",
				want, detail, p.text(".toast-note.error"))
		}
	}
}
