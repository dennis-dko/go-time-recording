package document

import "testing"

// A document is printed on a white page, whatever the screen it was read from
// looked like.
//
// The palette is taken from the browser's own variables, so somebody reading in
// the dark theme sends the dark theme's shades: near-white body text, pale grey
// captions, and a surface that is almost black. On a screen those are correct
// and on paper they are the opposite of correct - the text vanishes, and the
// empty part of every bar, which is drawn in the surface shade, comes out as a
// solid black block.
//
// That is what an exported evaluation actually looked like: a column of black
// bars with the figures beside them in a grey nobody could read.
func TestInkFromADarkScreenIsNotUsedOnWhitePaper(t *testing.T) {
	t.Parallel()

	dark := Palette{
		Accent: "#5b8dfa", Text: "#e8eaed", Muted: "#98a2ad",
		Border: "#2c333b", Surface: "#1c2126",
	}.resolve()

	// What is written with has to be readable against the page.
	if dark.text != defaultText {
		t.Errorf("near-white body text was kept for a white page: %v", dark.text)
	}

	if dark.muted != defaultMuted {
		t.Errorf("pale grey captions were kept for a white page: %v", dark.muted)
	}

	// What is filled with has to stay lighter than what is written on it.
	if dark.surface != defaultSurface {
		t.Errorf("an almost-black fill was kept, which is the black bar: %v", dark.surface)
	}

	if dark.border != defaultBorder {
		t.Errorf("an almost-black rule was kept: %v", dark.border)
	}

	// The accent survives, because it is the one shade that is about the
	// installation rather than about the screen's brightness - and this one is
	// legible on paper.
	if dark.accent == defaultAccent {
		t.Error("the accent was replaced although it can be read on a white page")
	}
}

// The light theme is what the document was designed against, so none of it is
// second-guessed.
func TestInkFromALightScreenIsUsedAsItIs(t *testing.T) {
	t.Parallel()

	light := Palette{
		Accent: "#2f6feb", Text: "#1c2126", Muted: "#626d78",
		Border: "#dfe3e8", Surface: "#ffffff",
	}.resolve()

	for _, c := range []struct {
		name string
		got  colour
		want colour
	}{
		{"accent", light.accent, colour{0x2f, 0x6f, 0xeb}},
		{"text", light.text, colour{0x1c, 0x21, 0x26}},
		{"muted", light.muted, colour{0x62, 0x6d, 0x78}},
		{"border", light.border, colour{0xdf, 0xe3, 0xe8}},
		{"surface", light.surface, colour{0xff, 0xff, 0xff}},
	} {
		if c.got != c.want {
			t.Errorf("%s was changed although it works on paper: got %v, want %v",
				c.name, c.got, c.want)
		}
	}
}

// An accent too pale to read is still replaced: the title and the filled part of
// every bar are drawn in it.
func TestAnAccentTooPaleForPaperIsReplaced(t *testing.T) {
	t.Parallel()

	if ink := (Palette{Accent: "#eaf1ff"}).resolve(); ink.accent != defaultAccent {
		t.Errorf("a near-white accent was kept for a white page: %v", ink.accent)
	}
}
