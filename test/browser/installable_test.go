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

// Chrome itself is asked whether the page can be installed as an app, which is
// the only judge whose answer matters: it reads the manifest, fetches the icons
// and checks their sizes, and says what stands in the way. A manifest that parses
// and an icon that decodes are what the unit cases hold; this holds that the
// browser agrees they add up to an app.
func TestChromeFindsThePageInstallable(t *testing.T) {
	t.Parallel()

	p := open(t)

	p.run("wait for the page", chromedp.WaitVisible("#form-login", chromedp.ByID))

	var problems []*cdppage.InstallabilityError

	// The manifest and the icons are fetched after the document, so the first
	// answer can be "not yet" - asked again until it settles or the patience runs
	// out.
	deadline := time.Now().Add(waitPatience)

	for {
		p.run("ask Chrome what stands in the way", chromedp.ActionFunc(func(ctx context.Context) error {
			var err error

			problems, err = cdppage.GetInstallabilityErrors().Do(ctx)

			return err
		}))

		if len(problems) == 0 || time.Now().After(deadline) {
			break
		}

		time.Sleep(500 * time.Millisecond)
	}

	var reasons []string

	for _, problem := range problems {
		reasons = append(reasons, problem.ErrorID)
	}

	if len(reasons) > 0 {
		t.Errorf("Chrome will not install the page: %s", strings.Join(reasons, ", "))
	}
}
