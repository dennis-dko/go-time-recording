//go:build browser

package browser

import (
	"context"
	"strings"
	"testing"
	"time"

	cdppage "github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// A first load that fails after the session was accepted keeps the reader
// signed in, and says what did not load.
//
// The page loads everything behind one catch, and that catch shows the sign-in
// form - its comment says there is no usable session. That is so when the
// session is what failed. When /me had accepted it and a later loader failed, a
// query the database refused or a connection that dropped part-way, a signed-in
// reader was shown the sign-in form with nothing saying what had gone wrong, and
// signing in again only opened a second session beside the first.
func TestAFirstLoadThatFailsAfterTheSessionKeepsTheReaderSignedIn(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	failTheProjects := chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := cdppage.AddScriptToEvaluateOnNewDocument(`(() => {
			const real = window.fetch;
			window.fetch = (url, options) =>
				/\/api\/v1\/projects(\?|$)/.test(String(url)) && (options?.method ?? 'GET') === 'GET'
					? Promise.resolve(new Response(
						JSON.stringify({ error: { message: 'the database refused one query' } }),
						{ status: 500, headers: { 'Content-Type': 'application/json' } }))
					: real(url, options);
		})()`).Do(ctx)

		return err
	})

	p.run("load the page again with the projects failing", failTheProjects, chromedp.Reload())

	// Done once the first load has concluded one way or the other: the sign-in
	// form out of its checking state, or a failure said in the corner.
	var concluded bool

	for deadline := time.Now().Add(waitPatience); !concluded && time.Now().Before(deadline); {
		p.run("see whether the first load has concluded", chromedp.Evaluate(`(() => {
			const screen = document.querySelector('#login-screen');
			return (!screen.hidden && !screen.classList.contains('checking'))
				|| Boolean(document.querySelector('.toast-note.error'));
		})()`, &concluded))

		if !concluded {
			time.Sleep(50 * time.Millisecond)
		}
	}

	if !concluded {
		t.Fatal("the first load never concluded")
	}

	if p.visible("#login-screen") {
		t.Error("a reader whose session was accepted was shown the sign-in form over it")
	}

	if toast := p.text(".toast-note.error .toast-text"); !strings.Contains(toast, "the database refused one query") {
		t.Errorf("the failure was not said: %q", toast)
	}
}

// A first load refused for want of a session leaves a sign-in that happened
// underneath it alone.
//
// The first load can fail because nobody is signed in yet and land after somebody
// has signed in on the form - the race showLogin already answers. Keeping a
// signed-in reader's screen when a loader fails must not take that for a failure
// worth a word: a 401 is the session refused, not a loader.
func TestAFirstLoadRefusedForWantOfASessionLeavesASignInAlone(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	var said int

	p.run("let a first load refused for want of a session land late", chromedp.Evaluate(`(() => {
		const before = document.querySelectorAll('.toast-note.error').length;
		afterAFailedFirstLoad(Object.assign(new Error('not signed in'), { status: 401 }));
		return document.querySelectorAll('.toast-note.error').length - before;
	})()`, &said))

	if said != 0 {
		t.Errorf("a first load refused for want of a session said %d failure(s) over a sign-in", said)
	}

	if p.visible("#login-screen") {
		t.Error("the sign-in screen went up over the session that was signed in underneath")
	}
}

// A first load whose session could not be checked shows the sign-in form and
// says why.
//
// The server answers a session it could not read - a database that did not
// answer - as a failure rather than as nobody signed in, and keeps the cookie, so
// a reload once the database is back carries on in the same session. The form is
// all this page can show without /me, but on its own it reads as the session
// having ended, which is the one thing that did not happen.
func TestAFirstLoadWhoseSessionCouldNotBeCheckedSaysSo(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	failTheSession := chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := cdppage.AddScriptToEvaluateOnNewDocument(`(() => {
			const real = window.fetch;
			window.fetch = (url, options) =>
				/\/api\/v1\/me(\?|$)/.test(String(url)) && (options?.method ?? 'GET') === 'GET'
					? Promise.resolve(new Response(
						JSON.stringify({ error: { message: 'the database did not answer', code: 'internal' } }),
						{ status: 500, headers: { 'Content-Type': 'application/json' } }))
					: real(url, options);
		})()`).Do(ctx)

		return err
	})

	p.run("load the page again with the session unreadable", failTheSession, chromedp.Reload())

	var concluded bool

	for deadline := time.Now().Add(waitPatience); !concluded && time.Now().Before(deadline); {
		p.run("see whether the first load has concluded", chromedp.Evaluate(`(() => {
			const screen = document.querySelector('#login-screen');
			return !screen.hidden && !screen.classList.contains('checking');
		})()`, &concluded))

		if !concluded {
			time.Sleep(50 * time.Millisecond)
		}
	}

	if !concluded {
		t.Fatal("the first load never concluded")
	}

	// Asked once the form is up, which is the same task the failure is said in.
	if toast := p.text(".toast-note.error .toast-text"); !strings.Contains(toast, "Could not load everything") {
		t.Errorf("the sign-in form went up over a session that could not be checked, and nothing said so: %q", toast)
	}
}
