//go:build browser

package browser

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// The viewer has to actually fill with lines, which means the poll ran, the
// response parsed and the rendering worked. A card that stays empty looks
// identical to one that is broken.
func TestTheLogViewerFillsWithLines(t *testing.T) {
	t.Parallel()

	// INFO, or the only lines would be the start-up warnings and there would be
	// nothing to prove polling works.
	p := openWith(t, "LOG_LEVEL=INFO")
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#log-card", chromedp.ByID))

	// The level chips are built from what the server reports, so their presence
	// proves the first request came back.
	p.waitForText("#log-levels", "ERROR")

	for _, level := range []string{"DEBUG", "INFO", "WARN", "ERROR"} {
		if !strings.Contains(p.text("#log-levels"), level) {
			t.Errorf("no filter offered for %s", level)
		}
	}

	waitForLines(p, "the log viewer never showed a line")

	// Searching narrows what is on screen. The server does the filtering, so this
	// is really asking whether the box reaches it - the filtering itself is
	// covered against the API. Folded in here rather than given its own case
	// because the expensive part is getting to this screen, and a second sign-in
	// and password change is most of a browser test's budget.
	p.run("search for something that cannot appear",
		chromedp.SendKeys("#log-search", "zzz-no-such-line-zzz", chromedp.ByID))

	// The search is debounced and then polled, so this waits rather than
	// asserting at once.
	deadline := time.Now().Add(waitPatience)
	for time.Now().Before(deadline) {
		if strings.TrimSpace(p.text("#log-output")) == "" {
			return
		}

		time.Sleep(250 * time.Millisecond)
	}

	t.Errorf("a search that matches nothing still shows lines:\n%s",
		truncateText(p.text("#log-output"), 400))
}

// Pausing has to stop the polling, or the button is decoration. Checked by the
// status line, which is what tells the reader whether what they are looking at
// is still moving.
func TestPausingTheLogViewer(t *testing.T) {
	t.Parallel()

	p := openWith(t, "LOG_LEVEL=INFO")
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#log-card", chromedp.ByID))

	waitForLines(p, "the log viewer never showed a line before pausing")

	p.run("pause", p.click("#log-pause"))

	// The button offering to resume is the state, and it is the same assertion in
	// either language the interface ships. Checking the status line's wording
	// would be checking a translation.
	deadline := time.Now().Add(waitPatience)
	for time.Now().Before(deadline) {
		label := strings.ToLower(p.text("#log-pause"))
		if strings.Contains(label, "resume") || strings.Contains(label, "fortsetzen") {
			return
		}

		time.Sleep(200 * time.Millisecond)
	}

	t.Errorf("the pause button still says %q, so nothing was paused", p.text("#log-pause"))
}

// A user must not be offered the log at all - not merely be refused when
// they ask. The whole Settings screen is the built-in administrator's.
func TestAUserIsNotOfferedTheLog(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()
	p.createOrdinaryAccount(t, "gerd@example.com", "gerd-password-1")

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn("gerd@example.com", "gerd-password-1")
	p.waitGone("#login-screen")
	p.settleWelcome()

	if p.visible("#tab-admin") {
		t.Error("a user is being offered the Settings tab, which holds the log")
	}
}

// waitForLines waits until the log output holds something.
func waitForLines(p *page, complaint string) {
	p.t.Helper()

	deadline := time.Now().Add(waitPatience)
	for time.Now().Before(deadline) {
		if strings.TrimSpace(p.text("#log-output")) != "" {
			return
		}

		time.Sleep(250 * time.Millisecond)
	}

	p.t.Fatalf("%s\n\nstatus: %q\n\napplication log:\n%s",
		complaint, p.text("#log-status"), p.app.Log())
}

// Coming back from a pause says so when more lines arrived than one page holds.
//
// The viewer asks for five hundred lines after the last one it has. When more
// matched, the answer kept the newest five hundred - the right ones to show in a
// burst - and moved the cursor past all of them, so the ones before were never
// shown and nothing said they were missing. The warning beside the output
// exists to say exactly that about lines the buffer has discarded; these it had
// not even discarded. A pause on a busy installation is the ordinary way to
// arrive there, and seven hundred requests make the lines.
func TestResumingAfterABurstSaysLinesWereSkipped(t *testing.T) {
	t.Parallel()

	p := openWith(t, "LOG_LEVEL=INFO")
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#log-card", chromedp.ByID))

	waitForLines(p, "the log viewer never showed a line before pausing")

	p.run("pause", p.click("#log-pause"))

	var made int

	p.run("make seven hundred lines", chromedp.Evaluate(`(async () => {
		const answers = await Promise.all(Array.from({ length: 700 },
			() => fetch('/api/v1/me', { credentials: 'same-origin' }).then((r) => r.status)));
		return answers.filter((s) => s === 200).length;
	})()`, &made, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
		return p.WithAwaitPromise(true)
	}))

	if made < 600 {
		t.Fatalf("only %d of the requests were answered, so this case cannot tell a gap", made)
	}

	p.run("resume", p.click("#log-pause"))

	var warned bool

	_ = chromedp.Run(p.ctx, chromedp.Poll(
		`!document.querySelector('#log-warning').hidden`, &warned,
		chromedp.WithPollingTimeout(15*time.Second)))

	if !warned {
		t.Errorf("the viewer came back from the pause showing the newest lines and "+
			"saying nothing about the ones before them; status: %q", p.text("#log-status"))
	}
}
