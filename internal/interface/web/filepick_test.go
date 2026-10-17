package web_test

import (
	"regexp"
	"strings"
	"testing"
)

// Every file chooser speaks the application's language, not the browser's.
//
// The browser draws its own file control - a button and a line saying no file has
// been chosen - in its own UI language, whatever the page is written in: measured
// on the settings screen, a German page showed "Choose File" and "No file chosen"
// beside the logo, on a browser started in English. It is the same reason the
// date fields write their own box. Every import already hides the control inside
// a label.file-pick that carries words of its own; the logo was the one left as
// the browser drew it.
func TestEveryFileChooserSpeaksTheApplicationsLanguage(t *testing.T) {
	markup := asset(t, "/")

	inputs := regexp.MustCompile(`<input[^>]*type="file"[^>]*>`).FindAllStringIndex(markup, -1)
	if len(inputs) == 0 {
		t.Fatal("no file inputs found in the markup, so this guard is checking nothing")
	}

	for _, at := range inputs {
		before := markup[:at[0]]
		opened := strings.LastIndex(before, "<label")
		closed := strings.LastIndex(before, "</label>")

		inPick := opened >= 0 && closed < opened &&
			strings.Contains(markup[opened:opened+strings.Index(markup[opened:], ">")], `class="file-pick"`)

		if !inPick {
			t.Errorf("%s is the browser's own file control, whose words are the browser's "+
				"language rather than the reader's: put it inside a label.file-pick with words "+
				"of its own", markup[at[0]:at[1]])
		}
	}
}
