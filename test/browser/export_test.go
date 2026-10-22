//go:build browser

package browser

import (
	"archive/zip"
	"path/filepath"
	"slices"
	"testing"

	"github.com/chromedp/cdproto/browser"
)

// Everything about the account leaves from My account as a ZIP, named in the
// reader's language, holding the account among its parts.
func TestEverythingAboutTheAccountLeavesFromMyAccount(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()
	p.showView("settings")

	into := t.TempDir()

	p.run("take downloads here", browser.SetDownloadBehavior(browser.SetDownloadBehaviorBehaviorAllow).
		WithDownloadPath(filepath.FromSlash(into)).WithEventsEnabled(true))

	p.run("ask for everything", p.click("#export-me"))

	saved := waitForDownload(t, into, ".zip")

	if name := filepath.Base(saved); len(name) < len("everything-about-me-") ||
		name[:len("everything-about-me-")] != "everything-about-me-" {
		t.Errorf("the archive is called %q, want it to start with everything-about-me-", name)
	}

	archive, err := zip.OpenReader(saved)
	if err != nil {
		t.Fatalf("what was saved is not a ZIP: %v", err)
	}

	defer func() { _ = archive.Close() }()

	var names []string
	for _, file := range archive.File {
		names = append(names, file.Name)
	}

	for _, want := range []string{"about.json", "account.json", "time-entries.json"} {
		if !slices.Contains(names, want) {
			t.Errorf("the archive holds %v, without %s", names, want)
		}
	}
}
