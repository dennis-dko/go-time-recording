//go:build browser

package browser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/dennis-dko/go-time-recording/internal/pkg/spreadsheet"
)

// The spreadsheet card: an export that downloads, and an import that shows what a
// file would do before it does it.
//
// The preview is the part worth driving in a browser. A file assembled by hand is
// wrong more often than it is right, and the whole point is that somebody sees
// which rows are refused and why, on screen, with the import button withheld until
// the file is clean.
func TestTheImportShowsWhatAFileWouldDoBeforeDoingIt(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	// A file with one good row and one that no ceiling allows, written through the
	// same writer the export uses.
	book, err := spreadsheet.Write([]spreadsheet.Row{
		{Date: time.Now(), Hours: 2, Description: "This one is fine"},
		{Date: time.Now().AddDate(0, 0, 1), Hours: 30, Description: "This one is not"},
	})
	if err != nil {
		t.Fatalf("building the workbook: %v", err)
	}

	path := filepath.Join(t.TempDir(), "entries.xlsx")
	if err := os.WriteFile(path, book, 0o600); err != nil {
		t.Fatalf("writing the workbook: %v", err)
	}

	p.run("choose the file",
		chromedp.Click(`.tab[data-view="timesheets"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#workbook-card", chromedp.ByID),
		chromedp.SetUploadFiles("#wb-file", []string{path}, chromedp.ByQuery),
	)

	p.waitShown("#wb-preview")

	if !p.visible("#wb-preview") {
		t.Fatal("choosing a file did not offer to check it")
	}

	p.run("check the file", p.click("#wb-preview"),
		chromedp.WaitVisible("#wb-preview-wrap", chromedp.ByID))

	deadline := time.Now().Add(waitPatience)
	for time.Now().Before(deadline) {
		if strings.Contains(p.text("#table-workbook tbody"), "This one is fine") {
			break
		}

		time.Sleep(200 * time.Millisecond)
	}

	shown := p.text("#table-workbook tbody")

	if !strings.Contains(shown, "This one is fine") {
		t.Fatalf("the preview does not show the file's rows: %q\n\napplication log:\n%s",
			shown, p.app.Log())
	}

	// The refused row says why, on its own line.
	var rejected int

	p.run("count the refused rows", chromedp.Evaluate(
		`document.querySelectorAll('#table-workbook tbody tr.rejected').length`, &rejected))

	if rejected != 1 {
		t.Errorf("%d row(s) are marked as refused, want 1", rejected)
	}

	if summary := p.text("#wb-summary"); !strings.Contains(summary, "1") {
		t.Errorf("the summary does not say how many rows are usable: %q", summary)
	}

	// And the import is withheld: offering it for a file that would be refused is
	// offering a failure.
	if p.visible("#wb-import") {
		t.Error("the import was offered for a file with a refused row in it")
	}

	// Nothing was written by looking.
	if entries := p.text("#table-timesheets tbody"); strings.Contains(entries, "This one is fine") {
		t.Error("the preview created entries")
	}

	// A clean file is offered, and goes through.
	clean, err := spreadsheet.Write([]spreadsheet.Row{
		{Date: time.Now(), Hours: 2, Description: "Imported from a file"},
	})
	if err != nil {
		t.Fatalf("building the second workbook: %v", err)
	}

	cleanPath := filepath.Join(t.TempDir(), "clean.xlsx")
	if err := os.WriteFile(cleanPath, clean, 0o600); err != nil {
		t.Fatalf("writing the second workbook: %v", err)
	}

	p.run("choose a clean file",
		chromedp.SetUploadFiles("#wb-file", []string{cleanPath}, chromedp.ByQuery))

	time.Sleep(300 * time.Millisecond)

	p.run("check it", p.click("#wb-preview"),
		chromedp.WaitVisible("#wb-import", chromedp.ByID))

	p.run("import it", p.click("#wb-import"))

	deadline = time.Now().Add(waitPatience)
	for time.Now().Before(deadline) {
		if strings.Contains(p.text("#table-timesheets tbody"), "Imported from a file") {
			return
		}

		time.Sleep(300 * time.Millisecond)
	}

	t.Fatalf("the imported entry never reached the table\n\ntable:\n%s\n\napplication log:\n%s",
		p.text("#table-timesheets tbody"), p.app.Log())
}
