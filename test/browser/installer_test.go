//go:build browser

package browser

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	cdppage "github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"github.com/dennis-dko/go-time-recording/test/harness"
)

// The installer says why it refused something in the reader's language.
//
// It has its own small dictionary because it runs before there is a database to
// ask anything, and its own labels were translated. What was not was the server's
// answer: the reasons are written in English where the rule is enforced, which is
// right for a log and wrong for the only screen this process has - so an
// otherwise entirely German page answered an empty field with
// "invalid field(s): name".
//
// Driven in a German browser, because that is the only thing this page has to go
// on - no session, no stored choice, no server to ask.
func TestTheInstallerRefusesInTheReadersLanguage(t *testing.T) {
	t.Parallel()

	app := harness.StartUnconfigured(t)

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("lang", "de-DE"),
		chromedp.NoSandbox,
	)

	// --lang is not enough on its own. It sets what Chrome asks servers for, and
	// on a machine that has the locale it also moves navigator.languages - but the
	// container CI runs has no German locale data, so the flag was accepted and
	// the page still saw an English browser. It passed here and failed there,
	// which is the least useful way for a test to be wrong.
	//
	// So the one thing this page reads is stated outright, before its own script
	// runs. That is also closer to what is being checked: not whether Chrome can
	// be talked into German, but whether the page does the right thing when the
	// browser asks for it.
	speakGerman := chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := cdppage.AddScriptToEvaluateOnNewDocument(
			`Object.defineProperty(navigator, 'languages',
				{ get: () => ['de-DE', 'de'], configurable: true });
			 Object.defineProperty(navigator, 'language',
				{ get: () => 'de-DE', configurable: true });`).Do(ctx)

		return err
	})

	if path := os.Getenv("CHROME_PATH"); path != "" {
		opts = append(opts, chromedp.ExecPath(path))
	}

	alloc, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancelAlloc()

	ctx, cancel := chromedp.NewContext(alloc)
	defer cancel()

	ctx, cancelTimeout := context.WithTimeout(ctx, 60*time.Second)
	defer cancelTimeout()

	var wrongToken, emptyName string

	if err := chromedp.Run(ctx,
		speakGerman,
		chromedp.Navigate(app.BaseURL()),
		chromedp.WaitVisible("#heading", chromedp.ByID),

		// A wrong token first: the refusal somebody meets most.
		chromedp.SendKeys("#token", "not-the-token", chromedp.ByID),
		chromedp.Click("#test", chromedp.ByID),
		chromedp.Sleep(1500*time.Millisecond),
		chromedp.Text("#note", &wrongToken, chromedp.ByID),

		// Then the real token, read out of the process's own log, and an empty
		// database name - which is the field refusal.
		chromedp.ActionFunc(func(ctx context.Context) error {
			m := regexp.MustCompile(`setup token: ([0-9a-f]+)`).FindStringSubmatch(app.Log())
			if m == nil {
				t.Fatalf("no setup token in the log: %.400s", app.Log())
			}

			return chromedp.Evaluate(
				`document.querySelector('#token').value = '`+m[1]+`';`+
					`document.querySelector('#name').value = ''`, nil).Do(ctx)
		}),
		chromedp.Click("#test", chromedp.ByID),
		chromedp.Sleep(2500*time.Millisecond),
		chromedp.Text("#note", &emptyName, chromedp.ByID),
	); err != nil {
		t.Fatalf("driving the installer: %v", err)
	}

	// The token is the refusal somebody meets most: it is a hex string copied out
	// of a log, and copying it wrongly is easy.
	if !strings.Contains(wrongToken, "Einrichtungs-Token") {
		t.Errorf("a wrong token is refused as %q, which is not the German this page "+
			"is otherwise written in", wrongToken)
	}

	// And the field list, which is where this was reported: it arrived as
	// "invalid field(s): name" - English, and naming the field by the key the
	// payload uses rather than by the label above it.
	if !strings.Contains(emptyName, "Datenbankname") {
		t.Errorf("an empty database name is refused as %q; it has to name the field "+
			"the way the label above it does", emptyName)
	}
}

// Answering the installer is the sign-in.
//
// Whoever fills this screen in has already proved more than a password does: the
// setup token is printed to this process's log, and what they decide with it is
// where every account, project and hour will be kept. Sending them on to a
// sign-in form to type the initial password out of the documentation establishes
// nothing that was not established a moment ago - and it is the step that puts
// "changeme123" in front of a person as a thing to remember.
//
// So the page hands itself over signed in, and what it lands on is the first
// screen that matters: choose a password. This drives the whole way through,
// because the two halves - the installer finishing, and the application coming
// up with a session already in the browser - only mean anything together.
func TestAnsweringTheInstallerLeavesTheBrowserSignedIn(t *testing.T) {
	t.Parallel()

	app := harness.StartUnconfigured(t)

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("lang", "en-US"),
		chromedp.NoSandbox,
	)

	if path := os.Getenv("CHROME_PATH"); path != "" {
		opts = append(opts, chromedp.ExecPath(path))
	}

	alloc, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancelAlloc()

	ctx, cancel := chromedp.NewContext(alloc)
	defer cancel()

	ctx, cancelTimeout := context.WithTimeout(ctx, 120*time.Second)
	defer cancelTimeout()

	var signedInAs string

	if err := chromedp.Run(ctx,
		chromedp.Navigate(app.BaseURL()),
		chromedp.WaitVisible("#heading", chromedp.ByID),

		// The token out of the process's own log, which is what an operator does.
		chromedp.ActionFunc(func(ctx context.Context) error {
			m := regexp.MustCompile(`setup token: ([0-9a-f]+)`).FindStringSubmatch(app.Log())
			if m == nil {
				t.Fatalf("no setup token in the log: %.400s", app.Log())
			}

			return chromedp.Evaluate(
				`document.querySelector('#token').value = '`+m[1]+`';`+
					`document.querySelector('#dialect').value = 'sqlite';`+
					`document.querySelector('#dialect').dispatchEvent(new Event('change'));`+
					`document.querySelector('#name').value = 'chosen'`, nil).Do(ctx)
		}),

		chromedp.Click("#save", chromedp.ByID),

		// The application takes the port in this same process, the page waits for
		// it and reloads - and what has to be waiting on the other side is the
		// interface rather than a sign-in form.
		chromedp.WaitVisible("#password-banner", chromedp.ByID),

		chromedp.Evaluate(`(async () => {
			const res = await fetch('/api/v1/me', { credentials: 'same-origin' });
			if (!res.ok) return 'not signed in: ' + res.status;

			const body = await res.json();

			return (body.data && body.data.user && body.data.user.email) || 'no account';
		})()`, &signedInAs, awaitPromise),
	); err != nil {
		t.Fatalf("driving the installer through to the application: %v\n\n%s",
			err, app.Log())
	}

	if signedInAs != "admin@local" {
		t.Errorf("after the installer the browser is %q rather than signed in as the "+
			"built-in administrator", signedInAs)
	}
}
