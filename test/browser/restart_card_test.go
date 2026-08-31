//go:build browser

package browser

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// Saving something that needs a restart puts a notice up, and the notice leads
// to the one place it can be acted on.
//
// The banner used to carry the whole thing - the list, the button and the
// explanation of what the button does here - which is a card's worth of screen
// stuck to the top of every page, saying what the card under Settings says. Now
// it says how many and offers the way there.
func TestTheRestartNoticeLeadsToTheCardThatDoesIt(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	// Nothing saved yet, so nothing is waiting and there is nothing to announce.
	if p.visible("#restart-banner") {
		t.Fatal("the restart notice is up on an installation with nothing waiting")
	}

	// Something only the next start reads.
	//
	// The trace exporter rather than the log level: the level is applied to the
	// running process the moment it is saved, so it never waits for anything -
	// which is exactly what this was written against on the first attempt.
	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-telemetry", chromedp.ByID))

	p.waitForFilled("#tel-tracing-hint")

	p.run("ask for tracing",
		chromedp.SetValue(`#form-telemetry [name="traceExporter"]`, "otlp", chromedp.ByQuery),
		chromedp.Evaluate(
			`document.querySelector('#form-telemetry [name="traceExporter"]')
				.dispatchEvent(new Event('change', { bubbles: true }))`, nil),
		chromedp.SetValue(`#form-telemetry [name="tracerUrl"]`, "jaeger:4317", chromedp.ByQuery),
		p.click(`#form-telemetry button[type="submit"]`))

	p.waitShown("#restart-banner")

	// It says how many rather than listing them: the list is on the card.
	if said := p.text("#restart-banner"); said == "" {
		t.Fatal("the restart notice is up and says nothing")
	}

	if p.count("#restart-banner .pending-list") != 0 {
		t.Error("the notice still lists what is waiting as well as the card")
	}

	if p.count("#restart-banner button#restart-now") != 0 {
		t.Error("the notice still carries a restart button of its own")
	}

	// Away from the settings screen, which is where somebody meets this notice.
	//
	// Users rather than the time entries: this is the built-in administrator,
	// which records no time and therefore has no such tab. Asking for one that
	// is not there fails as a click that never landed, which reads like the
	// notice covering the bar and is nothing of the kind.
	p.run("go and do something else", p.click(`.tab[data-view="users"]`),
		chromedp.WaitVisible("#table-users", chromedp.ByID))

	p.run("follow the notice", p.click("#restart-open"))

	// It lands on the settings screen, on a card that answers.
	p.waitShown("#restart-card")

	if !p.visible("#restart-card") {
		t.Fatal("following the notice did not reach the restart card")
	}

	if !p.visible("#restart-card-pending") {
		t.Error("the card does not list what is waiting, so the notice sent " +
			"somebody to a card that does not answer the question")
	}

	// And the card answers, which on this platform means one of two things.
	//
	// Not "there is a button": this suite runs on Windows, which has no execve
	// and therefore cannot restart itself, and the card is right to show the
	// reason in place of a control that would fail. What must never happen is
	// neither - a card somebody was sent to that says nothing.
	switch {
	case p.visible("#restart-card-now"):
		// It can be done from here, so the card says what doing it means.
		if said := p.text("#restart-card-mode"); said == "" {
			t.Error("the card offers a restart without saying what it would do here")
		}

	case p.visible("#restart-card-unsupported"):
		if said := p.text("#restart-card-unsupported"); said == "" {
			t.Error("the card withholds the button and gives no reason")
		}

	default:
		t.Error("the card offers neither a restart nor a reason there is none, so " +
			"the notice sent somebody to a card that answers nothing")
	}
}

// The restart card is above the version card.
//
// A restart is what stands between this installation and everything it has been
// given - including a version that has been downloaded and is waiting - so it is
// the first thing on the screen somebody was sent to, not something below the
// card that caused it.
func TestTheRestartCardComesBeforeTheVersionCard(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	// The version card, not the restart card: the restart card is only on screen
	// while something is waiting for one, and this asks about where the two sit
	// relative to each other - which is true of the markup whether either is
	// showing.
	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-telemetry", chromedp.ByID))

	var order string

	p.run("which comes first", chromedp.Evaluate(`
		(() => {
			const restart = document.querySelector('#restart-card');
			const version = document.querySelector('#update-card');

			if (!restart || !version) return 'missing';

			// compareDocumentPosition rather than offsets: one of the two may be
			// hidden, and a hidden card has no position to compare.
			return (restart.compareDocumentPosition(version)
				& Node.DOCUMENT_POSITION_FOLLOWING) ? 'restart first' : 'version first';
		})()`, &order))

	if order != "restart first" {
		t.Errorf("the cards are in the order %q", order)
	}
}

// Nobody who cannot administer the installation sees either of them.
func TestOnlyAnAdministratorIsToldAboutRestarts(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	// Long enough for a notice to have appeared if one were going to. There is
	// no condition to wait for here - the assertion is that something did not
	// happen - so the answer is only worth having after giving it a chance.
	time.Sleep(600 * time.Millisecond)

	if p.visible("#restart-banner") {
		t.Error("an ordinary account is told the installation is waiting for a " +
			"restart it cannot perform")
	}

	if p.visible("#restart-card") {
		t.Error("an ordinary account is offered a restart card")
	}

	// And the tab it would live on is not theirs either, which is the other half
	// of the same rule.
	if p.visible(`.tab[data-view="admin"]`) {
		t.Error("an ordinary account can reach the settings screen")
	}
}

// A version that came back different takes the page with it.
//
// The tab that pressed the button was the one tab that did not reload. Every
// other open one did: the announcement stream drops when the application goes
// away, reconnects to the new one, and reloads because the last thing it heard
// was that a restart was coming. The presser instead ran settleAfterRestart,
// which refreshed every card from the new server and left the script, the
// stylesheet and the markup in the tab exactly as the old version had built
// them - so an update that changed the interface showed nothing until somebody
// reloaded by hand.
//
// Worse than doing nothing, in fact: refreshAll restarts the announcement
// stream, and restarting it clears the record of what was last announced - so
// the reconnection that would have reloaded this tab found nothing to act on.
// The one tab that knew an update had happened was the one that forgot.
//
// Decided on the version rather than on which button was pressed. A restart
// that comes back as the same build changed no assets and has no business
// throwing away what is on screen; one that comes back as a different build has
// changed all of them.
func TestAnUpdateThatChangesTheVersionReloadsThePage(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	// Something to notice the reload by. It goes away with the document, and
	// nothing else on this page removes it.
	p.run("mark this document", chromedp.Evaluate(`window.__sameDocument = 1`, nil))

	// The answers a completed update produces, without an update: a process that
	// says it started at a different moment, and a build that says it is a
	// different version from the one this page was loaded from.
	p.run("answer as a new version", chromedp.Evaluate(`
		(() => {
			const server = api;

			api = (path, options) => {
				if (path === '/settings/restart') {
					return Promise.resolve({ startedAt: 'a-different-moment' });
				}

				if (path === '/branding') {
					return Promise.resolve({ ...lastBranding, version: 'v99.9.9' });
				}

				return server(path, options);
			};

			return 1;
		})()`, nil))

	p.run("settle after it", chromedp.Evaluate(
		`settleAfterRestart('whatever-it-was-before', 'done', 30000)`, nil))

	deadline := time.Now().Add(waitPatience)

	for time.Now().Before(deadline) {
		var same bool

		p.run("still the same document?", chromedp.Evaluate(
			`window.__sameDocument === 1`, &same))

		if !same {
			return
		}

		time.Sleep(200 * time.Millisecond)
	}

	t.Error("the page was still the document the old version built, so anything " +
		"the update changed about the interface stays invisible until somebody " +
		"reloads by hand")
}

// And a restart that comes back as the same build leaves the screen alone.
//
// Every card is refreshed from the process that just came back, which is the
// whole of what changed - throwing away the scroll position, the open card and
// whatever was half-typed would be a cost with nothing bought by it.
func TestARestartOntoTheSameVersionDoesNotReloadThePage(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("mark this document", chromedp.Evaluate(`window.__sameDocument = 1`, nil))

	p.run("answer as the same version", chromedp.Evaluate(`
		(() => {
			const server = api;

			api = (path, options) => path === '/settings/restart'
				? Promise.resolve({ startedAt: 'a-different-moment' })
				: server(path, options);

			return 1;
		})()`, nil))

	p.run("settle after it", chromedp.Evaluate(
		`settleAfterRestart('whatever-it-was-before', 'Restarted.', 30000)`, nil))

	p.waitForText("#toast", "Restarted.")

	var same bool

	p.run("still the same document?", chromedp.Evaluate(
		`window.__sameDocument === 1`, &same))

	if !same {
		t.Error("a restart onto the same build threw the page away, losing whatever " +
			"was on screen for no change at all")
	}
}
