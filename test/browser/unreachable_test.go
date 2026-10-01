//go:build browser

package browser

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// A request that never reached the server says so in the reader's language.
//
// Only one failure of fetch itself was put into words: the page's own timeout.
// Every other - the server stopped, the connection dropped, a laptop waking with
// no network - arrived at the caller as the browser's exception, so a German
// screen said "Failed to fetch" in Chrome and "NetworkError when attempting to
// fetch resource." in Firefox, which tells somebody nothing about what to do.
func TestARequestThatNeverArrivedSaysSoInTheReadersLanguage(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#update-card", chromedp.ByID))

	p.run("let the next check find no server", chromedp.Evaluate(`
		(() => {
			const real = window.fetch;
			window.fetch = (url, options) => String(url).includes('/settings/update/check')
				? Promise.reject(new TypeError('Failed to fetch'))
				: real(url, options);
			return 1;
		})()`, nil))

	p.run("press it", p.click("#update-check"))

	p.waitShown(".toast-note.error")

	toast := p.text(".toast-note.error .toast-text")

	if strings.Contains(toast, "Failed to fetch") {
		t.Errorf("the reader was shown the browser's own exception: %q", toast)
	}

	if !strings.Contains(toast, "could not be reached") {
		t.Errorf("the toast does not say the server could not be reached: %q", toast)
	}
}
