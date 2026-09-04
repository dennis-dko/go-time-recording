package web_test

import (
	"strings"
	"testing"
)

// A preview that describes a chosen file is forgotten when the file changes.
//
// Previews are drawn through redrawable, so a language change re-renders them
// without asking the server again, and stopRedrawing is how a card says the
// answer it was holding is gone. resetWorkbookCard states the reason: "The
// preview it described is gone, so there is nothing to draw again."
//
// Choosing a different file makes that equally true, and neither change handler
// said so. The draw stayed registered against the previous result, and the next
// language change put that verdict back on screen above a file nobody had
// checked - with the Import button back beside it if the verdict was clean,
// which would then upload the new file unseen.
//
// Only these two keys need it. Read through every redrawable call in app.js:
// release, announcement, branding, directorySync, update, reportChart, report
// and the date fields all draw a card that is refilled rather than emptied, and
// none is ever passed to stopRedrawing. The two here describe a file somebody
// chose, which is the only content that stops being true while the card is still
// on screen - so this checks the pair rather than demanding a stop for every key.
func TestAFileCardForgetsThePreviewItNoLongerDescribes(t *testing.T) {
	js := asset(t, "/app.js")

	for _, card := range []struct {
		what    string
		marker  string
		forgets string
	}{
		{
			"the timesheet card in the markup",
			"$('#wb-file').addEventListener('change'",
			"stopRedrawing('workbookPreview')",
		},
		{
			"the card buildSheetCard makes for projects, accounts and roles",
			"file.addEventListener('change'",
			"stopRedrawing(`sheetPreview:${spec.key}`)",
		},
	} {
		at := strings.Index(js, card.marker)
		if at < 0 {
			t.Errorf("app.js no longer contains %q, so %s is not being checked",
				card.marker, card.what)

			continue
		}

		// To the end of the handler, which is the first "});" after it - neither
		// body contains a nested one.
		handler := js[at:]
		if end := strings.Index(handler, "});"); end > 0 {
			handler = handler[:end]
		}

		if strings.Contains(handler, card.forgets) {
			continue
		}

		t.Errorf("%s hides its preview when another file is chosen without calling "+
			"%s, so the draw stays registered against the previous file and the "+
			"next language change puts that verdict back on screen",
			card.what, card.forgets)
	}
}
