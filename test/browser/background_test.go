//go:build browser

package browser

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// What the page asks by itself says so, and what somebody asks for does not.
//
// The server counts every request as the session being used unless it is told
// the page made it on its own, so an unmarked check on a timer keeps a session
// open on a screen nobody is at - the one the idle timeout is for. The minute's
// check is asked for here directly rather than waited for.
func TestThePageMarksTheRequestsItMakesByItself(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.run("record what is asked", chromedp.Evaluate(`(() => {
		window.asked = [];
		const real = window.fetch;
		window.fetch = (input, init) => {
			const url = typeof input === 'string' ? input : input.url;
			const headers = new Headers(init?.headers ?? {});
			window.asked.push({ url, background: headers.get('X-Background-Request') });
			return real(input, init);
		};
		return 1;
	})()`, nil))

	p.run("ask what the minute asks", chromedp.Evaluate(`void askWhetherTheScreenIsStillTrue()`, nil))
	p.atRest()

	p.run("ask for something on purpose", chromedp.Evaluate(`(async () => {
		await api('/timesheets?limit=1');
		return 1;
	})()`, nil, awaitPromise))

	var asked []struct {
		URL        string  `json:"url"`
		Background *string `json:"background"`
	}

	p.run("read what was asked", chromedp.Evaluate(`window.asked`, &asked))

	seen := map[string]bool{}

	for _, request := range asked {
		marked := request.Background != nil && *request.Background == "1"

		switch {
		case strings.Contains(request.URL, "/api/v1/me"), strings.Contains(request.URL, "/api/v1/maintenance"):
			seen[request.URL] = true

			if !marked {
				t.Errorf("the minute's check asked %s without saying the page asked it by itself", request.URL)
			}
		case strings.Contains(request.URL, "/api/v1/timesheets"):
			seen[request.URL] = true

			if marked {
				t.Errorf("a request asked for on purpose, %s, was marked as the page's own", request.URL)
			}
		}
	}

	if len(seen) < 3 {
		t.Fatalf("this case saw too little to say anything: %v", asked)
	}
}
