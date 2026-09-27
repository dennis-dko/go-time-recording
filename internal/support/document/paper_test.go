package document

import "testing"

// A document is printed on a white page, whatever the screen it was read from
// looked like.
//
// The palette arrives from the browser's own variables, so somebody reading in
// the dark theme sent near-white body text, pale grey captions and a surface
// almost black - correct on that screen, and the opposite of correct on paper.
//
// The rule is not "replace what cannot be read". That produced two different
// documents from one installation, depending on how the person exporting it
// happened to be reading: the light theme's shades were used as sent, the dark
// theme's were swapped out. A filed page should not carry the theme of whoever
// made it, so the page has its own ink and only the accent is taken.
func TestThePageHasItsOwnInkWhateverTheScreenSent(t *testing.T) {
	t.Parallel()

	for _, screen := range []struct {
		name    string
		palette Palette
	}{
		{"dark", Palette{
			Accent: "#5b8dfa", Text: "#e8eaed", Muted: "#98a2ad",
			Border: "#2c333b", Surface: "#1c2126",
		}},
		{"light", Palette{
			Accent: "#2f6feb", Text: "#1c2126", Muted: "#626d78",
			Border: "#dfe3e8", Surface: "#ffffff",
		}},
		{"nothing at all", Palette{}},
	} {
		ink := screen.palette.resolve()

		if ink.text != defaultText {
			t.Errorf("%s: the body is written in %v rather than the page's own ink",
				screen.name, ink.text)
		}

		if ink.muted != defaultMuted {
			t.Errorf("%s: captions are written in %v", screen.name, ink.muted)
		}

		if ink.border != defaultBorder {
			t.Errorf("%s: the table is ruled in %v", screen.name, ink.border)
		}

		if ink.surface != defaultSurface {
			t.Errorf("%s: the heading row is filled with %v", screen.name, ink.surface)
		}
	}
}

// The accent is the exception, because it is the installation's rather than the
// screen's - and it is still checked, since the title and every filled bar are
// drawn in it.
func TestTheAccentIsTakenFromTheInstallationAndStillHasToBeLegible(t *testing.T) {
	t.Parallel()

	if ink := (Palette{Accent: "#b45309"}).resolve(); ink.accent != (colour{0xb4, 0x53, 0x09}) {
		t.Errorf("an installation's own accent was not used: %v", ink.accent)
	}

	if ink := (Palette{Accent: "#eaf1ff"}).resolve(); ink.accent != defaultAccent {
		t.Errorf("a near-white accent was kept for a white page: %v", ink.accent)
	}
}
