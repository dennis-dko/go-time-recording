//go:build browser

package browser

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

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
	app := harness.StartUnconfigured(t)

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("lang", "de-DE"),
		chromedp.NoSandbox,
	)

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
