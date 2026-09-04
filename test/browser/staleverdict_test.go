//go:build browser

package browser

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/dennis-dko/go-time-recording/internal/pkg/spreadsheet"
)

// Choosing another file takes the last file's verdict off the screen for good.
//
// The preview is drawn through redrawable, so a language change re-renders it
// without sending the file again - and stopRedrawing is how a card says the
// answer it held is gone. resetWorkbookCard calls it and says why: "The preview
// it described is gone, so there is nothing to draw again." Choosing a different
// file makes exactly the same thing true - the change handler hides the preview
// and empties the summary - and does not call it.
//
// So the draw stayed registered against the old result, and the next language
// change put it back on screen: the verdict for a file nobody has chosen any
// more, sitting above a file nobody has checked. If that verdict was clean the
// Import button comes back with it, and it would upload the newly chosen file,
// whose rows have never been looked at. The card exists to show what a file
// would do before it does it.
//
// The same two lines are missing from the three cards buildSheetCard makes, and
// this drives the one in the markup because it is the one that can be reached
// without an administrator.
func TestChoosingAnotherFileDoesNotBringTheOldVerdictBack(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	write := func(name string, rows []spreadsheet.Row) string {
		t.Helper()

		book, err := spreadsheet.Write(rows)
		if err != nil {
			t.Fatalf("building %s: %v", name, err)
		}

		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, book, 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}

		return path
	}

	first := write("first.xlsx", []spreadsheet.Row{
		{Date: time.Now(), Hours: 2, Description: "The file that was checked"},
	})

	second := write("second.xlsx", []spreadsheet.Row{
		{Date: time.Now().AddDate(0, 0, 1), Hours: 3, Description: "The file that was not"},
	})

	p.run("check the first file",
		chromedp.Click(`.tab[data-view="timesheets"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#workbook-card", chromedp.ByID),
		chromedp.SetUploadFiles("#wb-file", []string{first}, chromedp.ByQuery))

	p.waitShown("#wb-preview")

	p.run("look at what it would do", p.click("#wb-preview"),
		chromedp.WaitVisible("#wb-preview-wrap", chromedp.ByID))

	p.run("choose a different file",
		chromedp.SetUploadFiles("#wb-file", []string{second}, chromedp.ByQuery))

	p.waitGone("#wb-preview-wrap")

	// The real trigger: a language change redraws every card that registered one.
	p.run("read the page in German", chromedp.Evaluate(`(() => {
		const picker = document.querySelector('#language-picker');
		picker.value = 'de';
		picker.dispatchEvent(new Event('change', { bubbles: true }));

		return picker.value;
	})()`, nil))

	// Long enough for the redraw to have happened if it was going to.
	time.Sleep(2 * time.Second)

	if p.visible("#wb-preview-wrap") {
		var shown string

		p.run("what came back", chromedp.Evaluate(
			`document.querySelector('#wb-summary').textContent.trim()`, &shown))

		t.Errorf("a language change put the previous file's verdict back on screen "+
			"(%q) while a different, unchecked file is chosen. The change handler "+
			"hides the preview without calling stopRedrawing, which is the line "+
			"resetWorkbookCard has and says the reason for", shown)
	}
}
