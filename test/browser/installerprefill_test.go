//go:build browser

package browser

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/dennis-dko/go-time-recording/test/harness"
)

// The installer fills in what the environment already supplied once it has
// been given the token, and not a moment before.
//
// An operator who set the database's host in a compose file and forgot the
// dialect lands here, and should not have to retype the rest - which is what
// the prefill is for. It used to arrive with the page, for anybody who could
// reach the port: the host, the database's name and the account that opens it,
// on a screen that asks for a token before it will do anything. They are asked
// for with the token now, so the form starts empty and fills when the token
// field is left - or at once, when the page was opened by the link in the log.
func TestTheInstallerFillsInTheEnvironmentsAnswersOnceItHasTheToken(t *testing.T) {
	t.Parallel()

	const token = "a-token-chosen-for-this-case"

	app := harness.StartUnconfigured(t,
		"SETUP_TOKEN="+token, "DB_HOST=db.internal", "DB_NAME=hours", "DB_USER=gtr")

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

	ctx, cancelTimeout := context.WithTimeout(ctx, 60*time.Second)
	defer cancelTimeout()

	const answers = `[document.querySelector('#host').value,
		document.querySelector('#name').value,
		document.querySelector('#user').value].join('|')`

	var before, after, byLink string

	// filled waits for the form to carry the environment's answers, which arrive
	// by a request of their own.
	filled := func(into *string) chromedp.Action {
		return chromedp.ActionFunc(func(ctx context.Context) error {
			deadline := time.Now().Add(10 * time.Second)

			for {
				if err := chromedp.Evaluate(answers, into).Do(ctx); err != nil {
					return err
				}

				if *into == "db.internal|hours|gtr" || time.Now().After(deadline) {
					return nil
				}

				time.Sleep(100 * time.Millisecond)
			}
		})
	}

	if err := chromedp.Run(ctx,
		chromedp.Navigate(app.BaseURL()),
		chromedp.WaitVisible("#heading", chromedp.ByID),

		// Long enough for a prefill that came with the page to have landed.
		chromedp.Sleep(1500*time.Millisecond),
		chromedp.Evaluate(answers, &before),

		// The token typed and the field left, as somebody does on the way to the form.
		chromedp.SendKeys("#token", token+"\t", chromedp.ByID),
		filled(&after),

		// And the link the log prints, which carries the token with it.
		chromedp.Navigate(app.BaseURL()+"/?token="+token),
		chromedp.WaitVisible("#heading", chromedp.ByID),
		filled(&byLink),
	); err != nil {
		t.Fatalf("driving the installer: %v", err)
	}

	if before != "||" {
		t.Errorf("before any token was given the form already held %q of the connection", before)
	}

	if after != "db.internal|hours|gtr" {
		t.Errorf("with the token typed the form holds %q; want what the environment supplied", after)
	}

	if byLink != "db.internal|hours|gtr" {
		t.Errorf("opened by the link in the log the form holds %q; want what the environment supplied", byLink)
	}
}
