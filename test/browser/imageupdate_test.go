//go:build browser

package browser

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// The version card says what the last image update came to, where it left this
// container running.
//
// The updater answers in a file - "none" when the registry had nothing newer,
// "failed: ..." when the pull or the recreate did not work - and the operations
// manual promised that the card would say so. Nothing read the file, so the card
// said nothing, and whoever pressed the button was left to find the updater's
// own log.
func TestTheUpdateCardSaysWhatTheLastImageUpdateCameTo(t *testing.T) {
	t.Parallel()

	// Our own feed: the point is what the card does with the updater's answer,
	// not whether GitHub is up.
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v99.0.0","body":"",` +
			`"html_url":"https://example.invalid/release","assets":[]}`))
	}))
	defer feed.Close()

	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "result"), []byte("none"), 0o600); err != nil {
		t.Fatalf("cannot leave the updater's answer: %v", err)
	}

	p := openWith(t, "UPDATE_FEED="+feed.URL, "GTR_UPDATE_REQUESTS="+dir)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#update-card", chromedp.ByID))

	if !p.visible("#update-image") {
		t.Fatal("the card says nothing about the last image update, which found " +
			"nothing newer")
	}

	if said := p.text("#update-image"); !strings.Contains(said, "nothing newer") {
		t.Errorf("the card says %q about an update that found nothing newer", said)
	}
}

// A wait for a restart ends as soon as the answer is that none is coming.
//
// The press that asks for a new image waits under an overlay for the
// application to come back as a different version, for up to five minutes,
// because a pull on a slow line takes minutes. When the updater's answer is that
// it changed nothing, no version is coming back - and the screen sat under the
// overlay for the whole five minutes and then called the application slow.
func TestAWaitForARestartEndsWhenTheAnswerIsThatNoneIsComing(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	// The wait compares start times, so it has to have one: without it the first
	// look would find a "different" process and end the wait for the wrong reason.
	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#update-card", chromedp.ByID))
	p.waitEvaluates("the start time is known", `String(Boolean(restartStartedAt))`, "true")

	var ended string

	p.run("wait for a restart that has been answered", chromedp.Evaluate(`(async () => {
		document.querySelector('#restart-overlay').hidden = false;
		const answered = async () => true;

		return Promise.race([
			settleAfterRestart(restartStartedAt, 'done', 60000, answered).then(() => 'ended'),
			new Promise((resolve) => { setTimeout(() => resolve('still waiting'), 5000); }),
		]);
	})()`, &ended, awaitPromise))

	if ended != "ended" {
		t.Errorf("the wait was %s five seconds after being told no restart is coming", ended)
	}

	if p.visible("#restart-overlay") {
		t.Error("the overlay is still over the screen after the wait ended")
	}
}
