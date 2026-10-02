//go:build browser

package browser

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	cdppage "github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"github.com/dennis-dko/go-time-recording/test/harness"
)

// Somebody in front of the installer is told what state the installation is in.
//
// The page had sentences for what it expects: a wrong token, an empty field, a
// connection that works. What it did not have was anything to say when the
// answer was not the installer's to give - and those are the moments somebody
// most needs telling, because nothing else on this screen can.

// speakingGerman states the one thing the installer's page reads to choose its
// language, before the page's own script runs.
//
// --lang is not enough on its own: it moves navigator.languages only on a
// machine that has the locale, and the container CI runs has none.
func speakingGerman() chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := cdppage.AddScriptToEvaluateOnNewDocument(
			`Object.defineProperty(navigator, 'languages',
				{ get: () => ['de-DE', 'de'], configurable: true });
			 Object.defineProperty(navigator, 'language',
				{ get: () => 'de-DE', configurable: true });`).Do(ctx)

		return err
	})
}

// setupToken is the token the instance printed, which is where an operator
// reads it.
func setupToken(t *testing.T, app *harness.App) string {
	t.Helper()

	m := regexp.MustCompile(`setup token: ([0-9a-f]+)`).FindStringSubmatch(app.Log())
	if m == nil {
		t.Fatalf("no setup token in the log: %.400s", app.Log())
	}

	return m[1]
}

// whatTheInstallerSays presses one of the installer's buttons and reads the
// note once the page has stopped saying that it is working.
func whatTheInstallerSays(button string, into *string) chromedp.Action {
	return chromedp.Tasks{
		// What the last press said is no answer to this one, and it would be read
		// as one if the wait below found it still standing.
		chromedp.Evaluate(`document.querySelector('#note').className = 'note'`, nil),
		chromedp.Click(button, chromedp.ByID),
		chromedp.WaitVisible(`#note.bad, #note.good`, chromedp.ByQuery),
		chromedp.Text("#note", into, chromedp.ByID),
	}
}

// installerInGerman opens the installer of app in a German browser.
func installerInGerman(t *testing.T, app *harness.App) (context.Context, func()) {
	t.Helper()

	alloc, cancelAlloc := chromedp.NewExecAllocator(context.Background(), launch("de-DE")...)
	ctx, cancel := chromedp.NewContext(alloc)
	ctx, cancelTimeout := context.WithTimeout(ctx, 90*time.Second)

	done := func() {
		cancelTimeout()
		cancel()
		cancelAlloc()
	}

	if err := chromedp.Run(ctx,
		speakingGerman(),
		chromedp.Navigate(app.BaseURL()),
		chromedp.WaitVisible("#heading", chromedp.ByID),
	); err != nil {
		done()
		t.Fatalf("opening the installer: %v", err)
	}

	return ctx, done
}

// A connection that cannot be opened is the commonest thing to go wrong on this
// screen, and it was the one refusal a German reader met in English from its
// first word: "cannot reach the database: dial tcp ...". The driver's words are
// nobody's to translate; the sentence around them is the page's.
func TestTheInstallerSaysInTheReadersLanguageThatAConnectionFailed(t *testing.T) {
	t.Parallel()

	app := harness.StartUnconfigured(t)

	// A port nothing listens on, so the refusal is immediate.
	closed := strconv.Itoa(harness.FreePort(t))

	ctx, done := installerInGerman(t, app)
	defer done()

	var said string

	if err := chromedp.Run(ctx,
		chromedp.Evaluate(
			`document.querySelector('#token').value = '`+setupToken(t, app)+`';`+
				`document.querySelector('#dialect').value = 'postgres';`+
				`document.querySelector('#dialect').dispatchEvent(new Event('change'));`+
				`document.querySelector('#name').value = 'gtr';`+
				`document.querySelector('#host').value = '127.0.0.1';`+
				`document.querySelector('#port').value = '`+closed+`';`+
				`document.querySelector('#user').value = 'gtr';`+
				`document.querySelector('#password').value = 'not-used'`, nil),
		whatTheInstallerSays("#test", &said),
	); err != nil {
		t.Fatalf("driving the installer: %v", err)
	}

	if !strings.Contains(said, "Die Verbindung konnte nicht hergestellt werden") {
		t.Errorf("a connection nothing answers is refused as %q, which does not say in "+
			"German that the connection failed", said)
	}

	// And the reason survives: which address refused is what somebody acts on.
	if !strings.Contains(said, closed) {
		t.Errorf("the refusal %q has lost what the driver said, which named port %s",
			said, closed)
	}
}
