//go:build browser

package browser

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// The log viewer stops asking while its tab is hidden, and catches up when it
// is looked at again.
//
// Every question it asks is a request, and at INFO every request is a line in
// the very buffer it shows. The other two things that poll in the background
// skip while the tab is hidden; this one went on, so a log screen left in a
// background tab overnight wrote a line every few seconds into a buffer of five
// thousand - and by morning the lines somebody would have come to read had been
// pushed out by the viewer's own asking.
func TestTheLogViewerStopsAskingWhileItsTabIsHidden(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open the log, asking every second", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#log-card", chromedp.ByID),
		chromedp.Evaluate(`(() => {
			window.logAsked = 0;
			const server = api;
			api = (path, options) => {
				if (String(path).startsWith('/admin/logs')) window.logAsked += 1;
				return server(path, options);
			};
			const delay = document.querySelector('#log-delay');
			delay.value = '1';
			delay.dispatchEvent(new Event('change'));
			return 1;
		})()`, nil))

	time.Sleep(2 * time.Second)

	p.run("hide the tab", chromedp.Evaluate(`(() => {
		Object.defineProperty(document, 'hidden', { get: () => true, configurable: true });
		Object.defineProperty(document, 'visibilityState', { get: () => 'hidden', configurable: true });
		document.dispatchEvent(new Event('visibilitychange'));
		window.logAsked = 0;
		return 1;
	})()`, nil))

	time.Sleep(4 * time.Second)

	var whileHidden int

	p.run("count", chromedp.Evaluate(`window.logAsked`, &whileHidden))

	if whileHidden > 0 {
		t.Errorf("the viewer asked %d time(s) in four seconds while its tab was hidden", whileHidden)
	}

	p.run("look at it again", chromedp.Evaluate(`(() => {
		Object.defineProperty(document, 'hidden', { get: () => false, configurable: true });
		Object.defineProperty(document, 'visibilityState', { get: () => 'visible', configurable: true });
		document.dispatchEvent(new Event('visibilitychange'));
		return 1;
	})()`, nil))

	time.Sleep(1500 * time.Millisecond)

	var afterwards int

	p.run("count again", chromedp.Evaluate(`window.logAsked`, &afterwards))

	if afterwards == 0 {
		t.Error("looked at again, the viewer did not ask for what it missed")
	}
}
