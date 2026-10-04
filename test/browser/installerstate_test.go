//go:build browser

package browser

import (
	"bytes"
	"context"
	"io"
	"net/http"
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

// An installer left open in a second tab, or on somebody else's screen, goes on
// looking like an installer after the first one has finished it. Its buttons
// then reach the application, whose refusal is in a shape this page did not
// read: it showed "[object Object]".
func TestAnInstallerLeftOpenSaysThatTheInstallationHasBeenSetUp(t *testing.T) {
	t.Parallel()

	app := harness.StartUnconfigured(t)
	token := setupToken(t, app)

	ctx, done := installerInGerman(t, app)
	defer done()

	// Somebody else finishes it: the same answer the page would send, from
	// outside this page.
	request, err := http.NewRequest(http.MethodPost, app.BaseURL()+"/install/save",
		bytes.NewBufferString(`{"dialect":"sqlite","name":"chosen"}`))
	if err != nil {
		t.Fatalf("building the answer: %v", err)
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Setup-Token", token)

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("answering the installer from elsewhere: %v", err)
	}

	_ = response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("the installer answered %d to a working connection", response.StatusCode)
	}

	// The application takes the port in the same process; wait until it is the
	// one answering, which only it can do with a version.
	deadline := time.Now().Add(60 * time.Second)

	for {
		branding, err := http.Get(app.BaseURL() + "/api/v1/branding")
		if err == nil {
			body, _ := io.ReadAll(branding.Body)
			_ = branding.Body.Close()

			if branding.StatusCode == http.StatusOK && bytes.Contains(body, []byte(`"version"`)) {
				break
			}
		}

		if time.Now().After(deadline) {
			t.Fatalf("the application did not take over within a minute:\n%s", app.Log())
		}

		time.Sleep(200 * time.Millisecond)
	}

	var said string

	if err := chromedp.Run(ctx,
		chromedp.Evaluate(
			`document.querySelector('#token').value = '`+token+`';`+
				`document.querySelector('#name').value = 'another'`, nil),
		whatTheInstallerSays("#test", &said),
	); err != nil {
		t.Fatalf("pressing the button on the page that was left open: %v", err)
	}

	if !strings.Contains(said, "inzwischen eingerichtet") {
		t.Errorf("the installer left open says %q once the installation has been set up "+
			"elsewhere, which does not tell its reader so", said)
	}
}

// A request that never arrives - the process stopped, the network gone - was
// reported in the browser's own words, which are English whatever the page is:
// "Failed to fetch".
func TestTheInstallerSaysSoWhenNothingAnswers(t *testing.T) {
	t.Parallel()

	app := harness.StartUnconfigured(t)
	token := setupToken(t, app)

	ctx, done := installerInGerman(t, app)
	defer done()

	// Skips where the platform cannot send the signal, as every case that stops
	// an instance does.
	app.Stop(t)

	var said string

	if err := chromedp.Run(ctx,
		chromedp.Evaluate(
			`document.querySelector('#token').value = '`+token+`';`+
				`document.querySelector('#name').value = 'chosen'`, nil),
		whatTheInstallerSays("#test", &said),
	); err != nil {
		t.Fatalf("pressing the button with nothing behind it: %v", err)
	}

	if !strings.Contains(said, "nicht erreichbar") {
		t.Errorf("with the process gone the installer says %q, which is not this page's "+
			"German for a server that could not be reached", said)
	}
}
