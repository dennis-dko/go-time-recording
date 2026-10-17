//go:build browser

package browser

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// A file chooser shows where the keyboard is.
//
// The input inside a label.file-pick is hidden by design - the label is what is
// drawn - so the browser's focus ring went round an element of no size, and Tab
// landed on nothing anybody could see. The switches had met the same thing and put
// the ring on what is drawn; the file choosers had not.
func TestAFileChooserShowsWhereTheKeyboardIs(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.run("open the time entries", p.click(`.tab[data-view="timesheets"]`))
	p.waitShown("#workbook-card")

	// A key first, so the focus that follows counts as the keyboard's.
	p.run("reach the file chooser from the keyboard", chromedp.KeyEvent(kb.Tab),
		chromedp.Evaluate(`document.querySelector('#wb-file').focus()`, nil))

	var state struct {
		Keyboard bool   `json:"keyboard"`
		Outline  string `json:"outline"`
	}

	p.evalJSON(`JSON.stringify((() => {
		const input = document.querySelector('#wb-file');
		const drawn = getComputedStyle(input.closest('.file-pick'));

		return { keyboard: input.matches(':focus-visible'), outline: drawn.outlineStyle + ' ' + drawn.outlineWidth };
	})())`, &state)

	if !state.Keyboard {
		t.Fatal("the file input did not take the keyboard's focus, so this case checks nothing")
	}

	if strings.HasPrefix(state.Outline, "none") {
		t.Errorf("with the keyboard on the file chooser, its label draws no ring (%s): "+
			"the focus is on an input of no size that nobody can see", state.Outline)
	}
}
