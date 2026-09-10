//go:build browser

// Package browser drives the real interface in a real browser.
//
// The integration tests prove the API answers correctly. They cannot prove
// anyone can *use* it: whether the sign-in screen actually goes away, whether a
// tab switch shows the right panel, whether a stylesheet rule quietly beats the
// hidden attribute. Those failures leave the API perfectly healthy and the
// application unusable.
//
// That is not hypothetical here. This project shipped a sign-in form that
// authenticated correctly and then left the overlay on screen, because
// `display: flex` on .login-screen won over the browser's own
// `[hidden] { display: none }`. Every API check passed. Only opening it in a
// browser showed it.
//
//	task test:browser
//	go test -tags browser ./test/browser
//
// Needs Chrome, Chromium or Edge. chromedp finds it; set CHROME_PATH if it is
// somewhere unusual.
package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	cdplog "github.com/chromedp/cdproto/log"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/dennis-dko/go-time-recording/test/harness"
)

// interactionTimeout bounds one test. Generous, because a cold browser start
// and the application's first migration both land inside it - and because the
// suite now runs several cases at once, where all of that happens on a machine
// doing several times the work.
//
// The case that measures a stopwatch spends forty of these seconds deliberately
// asleep, waiting out the smallest bookable duration; on a busy two-core runner
// the rest of its work has to fit in what is left.
const interactionTimeout = 150 * time.Second

// waitPatience is how long the helpers below wait for something that is on its
// way.
//
// Generous on purpose. These were written against a quiet machine running one
// case at a time; the suite now runs several at once, where the same work takes
// several times as long in wall-clock terms without anything being wrong. A wait
// that expires under that load reports a timeout, which reads like a broken
// feature rather than a busy machine.
//
// It costs nothing when the thing arrives - every one of these returns the
// moment its condition holds - and only lengthens how long a genuine failure
// takes to announce itself. The per-case ceiling above is what bounds that.
const waitPatience = 45 * time.Second

func TestMain(m *testing.M) {
	cleanup, err := harness.Build()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	code := m.Run()

	cleanup()
	os.Exit(code)
}

// page is a browser pointed at a fresh instance.
type page struct {
	t   *testing.T
	ctx context.Context
	app *harness.App

	// What the page threw, collected as it happens. Guarded because the listener
	// runs on chromedp's own goroutine.
	mu           sync.Mutex
	scriptErrors []string
}

// open starts an instance and a browser, and loads the interface.
func open(t *testing.T) *page {
	t.Helper()

	return openWith(t)
}

// openWith is open with extra environment for the instance, for the cases that
// need the application configured differently - the log viewer needs a log level
// that actually produces lines.
func openWith(t *testing.T, env ...string) *page {
	t.Helper()

	app := harness.Start(t, env...)

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),

		// Pinned, because the interface writes figures and dates the way the
		// reader's own browser writes them - so without this the suite asserts
		// against whatever locale the machine running it happens to have. Five
		// cases search the time table for "1.11" and found "1,11" on a German
		// machine: the application was right and the suite was not portable, which
		// is worse, because it trains whoever runs it locally to expect red.
		//
		// CI is en-US and was passing by luck rather than by decision. Stating it
		// here makes that a decision, and the one case that is *about* following
		// the browser compares against that browser's own Intl rather than against
		// a format written down here, so it holds whatever this is set to.
		chromedp.Flag("lang", "en-US"),

		// A window somebody might actually use.
		//
		// chromedp's default is 764px wide, which is narrower than any desktop and
		// wide enough not to be a phone. The interface is responsive, so nothing
		// was broken by it - but the top bar wraps into three rows at that width,
		// and a suite whose geometry is an accident of a default is a suite that
		// disagrees with the machine next to it. The one case that is about narrow
		// screens sets its own size.
		chromedp.WindowSize(1280, 900),

		// The container images CI uses run as root, where Chrome refuses to
		// start without this.
		chromedp.NoSandbox,
	)

	if path := os.Getenv("CHROME_PATH"); path != "" {
		opts = append(opts, chromedp.ExecPath(path))
	}

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	t.Cleanup(cancelAlloc)

	ctx, cancel := chromedp.NewContext(allocCtx)
	t.Cleanup(cancel)

	ctx, cancelTimeout := context.WithTimeout(ctx, interactionTimeout)
	t.Cleanup(cancelTimeout)

	p := &page{t: t, ctx: ctx, app: app}
	p.watchForScriptErrors()

	p.run("load the interface",
		// So the browser reports what it objects to. Without it a script that
		// cannot be parsed is silent here and shows up as every case timing out.
		cdplog.Enable(),
		chromedp.Navigate(app.BaseURL()),
		chromedp.WaitVisible("#form-login", chromedp.ByID),
	)

	return p
}

// watchForScriptErrors records anything the page throws.
//
// Without this a script that will not parse looks like this: every case in the
// suite fails, each after ninety seconds, saying "load the interface: context
// deadline exceeded" - which describes a page that did not appear and says
// nothing about why. The browser knew exactly why, and threw it away.
//
// A missing semicolon cost two runs of the whole suite before anyone looked at
// the file itself. The engine had the answer at once: SyntaxError, with the line
// number.
//
// Cheap, and it applies to every failure rather than to syntax: an exception in a
// click handler leaves a screen that simply does not respond, which reads exactly
// like a selector that no longer matches.
func (p *page) watchForScriptErrors() {
	chromedp.ListenTarget(p.ctx, func(event any) {
		var recorded string

		switch e := event.(type) {
		case *runtime.EventExceptionThrown:
			// Something that ran and threw: a click handler, a failed await.
			details := e.ExceptionDetails
			recorded = fmt.Sprintf("%s (%s:%d:%d)",
				details.Text, details.URL, details.LineNumber, details.ColumnNumber)

		case *cdplog.EventEntryAdded:
			// Everything the browser itself complains about, and the one that
			// matters most is here rather than above: a script that will not
			// *parse* never runs, so it throws nothing. It is reported as a log
			// entry, which is why listening only for thrown exceptions caught none
			// of it - and a script that does not parse is the failure that takes
			// the whole suite down at once.
			if e.Entry == nil || e.Entry.Level != cdplog.LevelError {
				return
			}

			recorded = fmt.Sprintf("%s (%s:%d)", e.Entry.Text, e.Entry.URL, e.Entry.LineNumber)
		default:
			return
		}

		p.mu.Lock()
		defer p.mu.Unlock()

		p.scriptErrors = append(p.scriptErrors, recorded)
	})
}

// thrown is what the page threw, if anything.
func (p *page) thrown() string {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.scriptErrors) == 0 {
		return ""
	}

	return "\n\nthe page threw:\n" + strings.Join(p.scriptErrors, "\n")
}

// complaints is everything the browser objected to, for a case that wants to
// assert the page was quiet rather than merely working.
//
// watchForScriptErrors collects these on every page, but thrown() only ever
// shows them as extra context on a step that has already failed. Nothing asked
// the question on its own, so a page that produces the right result while the
// browser files errors about how it got there passed in silence - which is
// exactly the shape of a Content-Security-Policy violation: the feature works,
// and the console fills up.
func (p *page) complaints() []string {
	p.mu.Lock()
	defer p.mu.Unlock()

	return slices.Clone(p.scriptErrors)
}

// evalJSON runs an expression that returns JSON and unpacks it.
//
// Walking a map[string]any of chromedp's structured values in Go is far less
// readable than the thing being asserted deserves. The page stringifies, this
// unpacks, and the case reads like a case.
func (p *page) evalJSON(expression string, into any) {
	p.t.Helper()

	var raw string

	p.run("read the page's answer", chromedp.Evaluate(expression, &raw))

	if err := json.Unmarshal([]byte(raw), into); err != nil {
		p.t.Fatalf("reading the page's answer: %v\n\n%.400s", err, raw)
	}
}

// run executes actions and fails the test with the application's log attached,
// which is usually where the reason is.
func (p *page) run(what string, actions ...chromedp.Action) {
	p.t.Helper()

	if err := chromedp.Run(p.ctx, actions...); err != nil {
		p.t.Fatalf("%s: %v%s\n\napplication log:\n%s",
			what, err, p.thrown(), p.app.Log())
	}
}

// awaitPromise makes chromedp wait for an async evaluation to resolve instead
// of handing back the pending promise.
func awaitPromise(ep *runtime.EvaluateParams) *runtime.EvaluateParams {
	return ep.WithAwaitPromise(true)
}

// state describes what the page looked like, for a wait that ran out of time.
//
// "It never appeared" and "it appeared and something took it away again" fail
// the same way and are fixed in completely different places, and the log alone
// separates them only when the server was involved. This says which screen is
// up, what the tabs offer, whether the load finished and what is still in
// flight - enough to tell a click that never landed from one that was undone.
func (p *page) state() string {
	p.t.Helper()

	var out string

	p.run("describe the page", chromedp.Evaluate(`JSON.stringify({
		loaded: document.documentElement.dataset.loaded ?? null,
		shown: [...document.querySelectorAll('.view')].filter(v => !v.hidden).map(v => v.id),
		activeTab: document.querySelector('.tab.active')?.dataset.view ?? null,
		tabs: [...document.querySelectorAll('.tab')].filter(v => !v.hidden).map(v => v.dataset.view),
		dialogs: [...document.querySelectorAll('dialog[open]')].map(v => v.id || v.className),
		hash: location.hash,
		inFlight: typeof progress === 'object' ? progress.inFlight : null,
		notice: document.querySelector('#toast')?.textContent?.trim()?.slice(0, 160) ?? '',
		overTabs: [...document.querySelectorAll('.tab')].filter(t => !t.hidden).map(t => {
			const box = t.getBoundingClientRect();
			const top = document.elementFromPoint(box.x + box.width / 2, box.y + box.height / 2);
			if (top === t || t.contains(top)) return t.dataset.view + ':ok';

			// The owning element's id, not only the class of whatever pixel was
			// hit: "overlay-card" names a shape, and what matters is which
			// overlay it belongs to.
			const owner = top?.closest?.('[id]');

			return t.dataset.view + ':' + (owner?.id || top?.className || 'nothing');
		}),
	})`, &out))

	return out
}

// jsBroken reports whether app.js failed to initialise.
//
// A page whose script threw on load still renders its markup, so it looks
// almost right: the tabs are there and nothing responds. This asks the page
// whether the script got far enough to define anything.
//
// What the browser complained about on the way is a separate question, and
// complaints() is where it is asked.
func (p *page) jsBroken() bool {
	p.t.Helper()

	var ok bool

	// If app.js failed to parse or threw during init, the functions it defines
	// are not there.
	p.run("check that the script initialised",
		chromedp.Evaluate(`typeof window.gtrTheme === 'object'`, &ok))

	return !ok
}
