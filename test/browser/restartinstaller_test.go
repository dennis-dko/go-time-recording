//go:build browser

package browser

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// The screen waiting out a restart recognises the installer when that is what
// came back, and goes to it.
//
// A restart reads the configuration afresh, so one with no connection left comes
// back as the installer - the integration suite restarts a real process into it.
// The installer answers every path with its page, so the wait read it as an
// application not up yet, waited out its minute, and then said the application
// may still be starting: of something that had been answering all along, and was
// waiting for its token.
//
// The two answers are given here as the installer gives them - a page where the
// application's JSON was expected, and JSON at /install/state - because a real
// restart cannot be had on this platform, and what is under test is what the
// screen makes of them.
func TestTheRestartWaitRecognisesTheInstaller(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("wait out a restart that comes back as the installer", chromedp.Evaluate(`(() => {
		window.cameBackTo = 'this page';

		const server = api;
		api = (path, options) => path === '/settings/restart'
			? Promise.reject(new Error('a page came back where JSON was expected'))
			: server(path, options);

		const network = window.fetch;
		window.fetch = (url, options) => String(url).endsWith('/install/state')
			? Promise.resolve(new Response(JSON.stringify({ appName: 'Time Recording' }),
				{ headers: { 'Content-Type': 'application/json' } }))
			: network(url, options);

		settleAfterRestart('the start before', 'done', 6000);

		return 1;
	})()`, nil))

	// A reload is what goes to the installer, and it takes the marker with it.
	deadline := time.Now().Add(4 * time.Second)

	for time.Now().Before(deadline) {
		var marker string

		if err := chromedp.Run(p.ctx, chromedp.Evaluate(`String(window.cameBackTo)`, &marker)); err == nil &&
			marker == "undefined" {
			return
		}

		time.Sleep(200 * time.Millisecond)
	}

	t.Error("the screen went on waiting for the application while the installer answered")
}
