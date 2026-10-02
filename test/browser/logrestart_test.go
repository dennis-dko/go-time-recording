//go:build browser

package browser

import (
	"runtime"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// A viewer left open across a restart is shown the new process's log from its
// beginning, and told that is what it is looking at.
//
// The viewer asks for "everything after" the last line it holds, and the
// numbers start again with the process. Left open across a restart - which the
// interface survives without a reload when the same build comes back, and which
// a service manager does without asking anybody - it asked the new process for
// lines after a number only the old one had reached, was answered with nothing,
// and never showed what the new process wrote while it started.
//
// A restart is not staged here: this suite runs on Windows as well, where a
// process cannot replace its own image, and what the viewer has to do does not
// need one. It holds a position under another process's name, which is all a
// restart leaves it with. TestTheLogViewerFollowsARealRestart is the same with
// nothing staged, where the platform allows it.
func TestTheLogViewerSaysWhenTheProcessUnderItHasChanged(t *testing.T) {
	t.Parallel()

	p := openWith(t, "LOG_LEVEL=INFO")
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#log-card", chromedp.ByID))

	waitForLines(p, "the log viewer never showed a line")

	if p.visible("#log-warning") {
		t.Fatalf("the viewer warns before anything has happened: %q", p.text("#log-warning"))
	}

	var named bool

	p.run("check that the viewer keeps the process's name",
		chromedp.Evaluate(`typeof logView.epoch === 'string' && logView.epoch !== ''`, &named))

	if !named {
		t.Fatal("the viewer does not keep the name of the process its position was counted by")
	}

	// What a restart leaves an open viewer holding.
	p.run("hold a position from another process",
		chromedp.Evaluate(`logView.epoch = 'the-process-before-this-one'`, nil))

	p.waitForText("#log-warning", "started again")

	// A viewer that comes back late - paused, or in a tab the browser put to
	// sleep - follows on into a log that has been written for hours, so the page
	// it is handed has begun again, lost its first lines and left more out than
	// it shows, all at once. The notice said one of the three and stopped, which
	// announced the log of the new process over a page that began hundreds of
	// lines into it. Asked of the function that words it, because the state
	// itself takes a replaced process and then five thousand lines to reach.
	var late, unbroken string

	p.run("word a page with every gap, and one with none",
		chromedp.Evaluate(`logGaps({ restarted: true, dropped: 40, skipped: 300 })`, &late),
		chromedp.Evaluate(`logGaps({ restarted: false, dropped: 0, skipped: 0 })`, &unbroken))

	for _, gap := range []string{"started again", "discarded from the buffer", "300 were passed over"} {
		if !strings.Contains(late, gap) {
			t.Errorf("a page that began again, lost lines and left 300 out is worded %q, "+
				"which does not say %q", late, gap)
		}
	}

	if unbroken != "" {
		t.Errorf("a page with no gap in it is worded %q", unbroken)
	}
}

// The same with nothing staged: the process under an open viewer really is
// replaced, and the viewer shows what the new one wrote while it started.
//
// The line a start writes once - which version is starting, on which database -
// is in the viewer once before and twice after, and that second one is the line
// the unfixed viewer never showed: it took up the new log where its old
// position happened to fall.
func TestTheLogViewerFollowsARealRestart(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("a process cannot replace its own image on Windows")
	}

	p := openWith(t, "LOG_LEVEL=INFO")
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#log-card", chromedp.ByID))

	waitForLines(p, "the log viewer never showed a line")

	const starts = `[...document.querySelectorAll('#log-output .log-line')]
		.filter((line) => line.textContent.includes('starting (dialect=')).length`

	var before int

	p.run("count the starts the viewer shows", chromedp.Evaluate(starts, &before))

	if before != 1 {
		t.Fatalf("the viewer shows %d starts of a process that has started once", before)
	}

	p.run("restart the application under the viewer",
		chromedp.Evaluate(`api('/settings/restart', { method: 'POST' }).then(() => true)`, nil, awaitPromise))

	p.waitForText("#log-warning", "started again")

	var after int

	p.run("count the starts again", chromedp.Evaluate(starts, &after))

	if after != 2 {
		t.Errorf("after a restart the viewer shows %d starts; want the old process's and the new one's\n\n%s",
			after, p.text("#log-output"))
	}
}
