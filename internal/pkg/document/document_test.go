package document_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/pkg/document"
)

// What can be asserted about a PDF without a PDF reader.
//
// The text cannot: it is written with a subset of an embedded TrueType font, so
// the words in the file are glyph numbers rather than letters, and looking for
// "Stunden" in the bytes would fail on a document that renders perfectly. The
// structure can, and it is what these tests are about - a page is a page, an
// image is an image, and an embedded font is the thing that decides whether a
// name with an umlaut in it arrives or is quietly dropped.
const (
	// Every PDF starts and ends with these.
	pdfHeader = "%PDF-"
	pdfEnd    = "%%EOF"

	// One per page, plus one for the /Pages node that lists them.
	pageMarker  = "/Type /Page"
	pagesMarker = "/Type /Pages"

	// An embedded TrueType font. A document using fpdf's built-in fonts has
	// none of these.
	embeddedFont = "FontFile2"

	imageMarker = "/Subtype /Image"
)

// pages counts the pages, discounting the node that lists them.
func pages(pdf []byte) int {
	return bytes.Count(pdf, []byte(pageMarker)) - bytes.Count(pdf, []byte(pagesMarker))
}

// aChart is a picture of the size and shape the interface sends.
func aChart(t *testing.T, width, height int) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))

	for x := range width {
		for y := range height {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: 80, B: 160, A: 255})
		}
	}

	var out bytes.Buffer

	if err := png.Encode(&out, img); err != nil {
		t.Fatalf("cannot draw a test chart: %v", err)
	}

	return out.Bytes()
}

func aMoment() time.Time { return time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC) }

// The ordinary case: a report as the screen had it, with its chart and its
// figures, on one page.
func TestAnEvaluationBecomesAPDFWithItsChartInIt(t *testing.T) {
	t.Parallel()

	out, err := document.Write(document.Document{
		Title:    "Auswertung",
		Subtitle: "1. August 2026 - 31. August 2026",
		Sections: []document.Section{{
			Heading: "Stunden pro Projekt",
			Caption: "Balken",
			Chart:   aChart(t, 800, 400),
			Table: &document.Table{
				Columns: []string{"Zeitraum", "Stunden"},
				Numeric: []bool{false, true},
				Rows:    [][]string{{"August 2026", "12,5"}, {"Juli 2026", "8,0"}},
			},
		}},
		Summary: []document.Line{{Label: "Gesamt", Value: "20,5"}},
		Footer:  "Zeiterfassung",
		Written: aMoment(),
	})
	if err != nil {
		t.Fatalf("writing the document: %v", err)
	}

	if !bytes.HasPrefix(out, []byte(pdfHeader)) {
		t.Errorf("this is not a PDF; it starts %q", out[:min(16, len(out))])
	}

	if !bytes.Contains(out, []byte(pdfEnd)) {
		t.Error("the document has no end marker, so a reader will call it damaged")
	}

	if got := pages(out); got != 1 {
		t.Errorf("a report of two rows should be one page; it is %d", got)
	}

	if got := bytes.Count(out, []byte(imageMarker)); got != 1 {
		t.Errorf("the chart is not in the document: %d images", got)
	}
}

// The reason for carrying fonts rather than using the ones every PDF reader
// already has.
//
// fpdf's built-in fonts are Latin-1. They cover German and turn everything else
// into question marks without saying so - and a project name is whatever
// somebody typed into the box. This asserts the fonts are embedded, which is
// what makes the difference; it cannot assert the glyphs, because that is what
// a reader does.
func TestTheFontsTravelWithTheDocument(t *testing.T) {
	t.Parallel()

	out, err := document.Write(document.Document{
		Title:   "Übersicht",
		Footer:  "Zeiterfassung",
		Written: aMoment(),
		Sections: []document.Section{{
			Heading: "Projekte",
			Table: &document.Table{
				Columns: []string{"Projekt", "Stunden"},
				Numeric: []bool{false, true},
				Rows:    [][]string{{"Straßenbau Süd", "3,5"}, {"Проект", "1,0"}},
			},
		}},
	})
	if err != nil {
		t.Fatalf("writing the document: %v", err)
	}

	if got := bytes.Count(out, []byte(embeddedFont)); got < 1 {
		t.Error("no font is embedded, so anything outside Latin-1 will arrive as " +
			"question marks and nobody will be told")
	}
}

// A month of daily figures is longer than a page, and the reader of page two
// needs to know which column is which.
func TestALongTableRunsOnAndTakesItsHeadingsWithIt(t *testing.T) {
	t.Parallel()

	rows := make([][]string, 0, 120)

	for i := range 120 {
		rows = append(rows, []string{time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).
			AddDate(0, 0, i).Format("2006-01-02"), "8,0"})
	}

	out, err := document.Write(document.Document{
		Title:   "Auswertung",
		Written: aMoment(),
		Sections: []document.Section{{
			Heading: "Stunden pro Tag",
			Table: &document.Table{
				Columns: []string{"Zeitraum", "Stunden"},
				Numeric: []bool{false, true},
				Rows:    rows,
			},
		}},
	})
	if err != nil {
		t.Fatalf("writing the document: %v", err)
	}

	if got := pages(out); got < 2 {
		t.Fatalf("120 rows fitted on %d page(s), which means they were not all written", got)
	}
}

// The statistics screen sends two charts at once, and they are two different
// pictures.
//
// Written after the first attempt named each registered image after its
// heading: fpdf keeps them in a map, so two sections sharing a heading shared
// one picture, and the second chart was silently the first one again.
func TestTwoChartsAreTwoPictures(t *testing.T) {
	t.Parallel()

	same := "Stunden"

	out, err := document.Write(document.Document{
		Title:   "Statistik",
		Written: aMoment(),
		Sections: []document.Section{
			{Heading: same, Chart: aChart(t, 800, 300)},
			{Heading: same, Chart: aChart(t, 600, 600)},
		},
	})
	if err != nil {
		t.Fatalf("writing the document: %v", err)
	}

	if got := bytes.Count(out, []byte(imageMarker)); got != 2 {
		t.Errorf("two charts became %d picture(s) in the file", got)
	}
}

// Neither part is required: overtime is three figures and no picture at all.
func TestADocumentWithNeitherChartNorTableIsStillADocument(t *testing.T) {
	t.Parallel()

	out, err := document.Write(document.Document{
		Title:    "Überstunden",
		Subtitle: "1. Januar 2026 - 31. Dezember 2026",
		Summary: []document.Line{
			{Label: "Soll", Value: "1.680,0"},
			{Label: "Ist", Value: "1.712,5"},
			{Label: "Saldo", Value: "+32,5"},
		},
		Written: aMoment(),
	})
	if err != nil {
		t.Fatalf("writing the document: %v", err)
	}

	if got := pages(out); got != 1 {
		t.Errorf("three figures should be one page; they are %d", got)
	}
}

// Something that is not a picture is refused, and the refusal says what was
// wrong with it rather than arriving as a damaged file.
func TestAChartThatIsNotAPictureIsRefused(t *testing.T) {
	t.Parallel()

	_, err := document.Write(document.Document{
		Title:    "Auswertung",
		Written:  aMoment(),
		Sections: []document.Section{{Heading: "Stunden", Chart: []byte("not a picture")}},
	})

	if err == nil {
		t.Fatal("thirteen bytes of prose were accepted as a chart")
	}

	if !strings.Contains(err.Error(), "PNG") {
		t.Errorf("the refusal does not say what was wrong: %v", err)
	}
}

// A description far wider than its column is cut rather than pushing the
// column beside it off the paper.
//
// Asserted through the page count, which is the only thing visible from out
// here: text that overflows its cell in fpdf does not wrap, it runs on, and the
// document is still one page either way. What this really guards is that a
// pathological cell does not throw or hang - the shortening loop walks one rune
// at a time, and an empty result would loop forever.
func TestAnAbsurdlyWideCellDoesNotDerailTheTable(t *testing.T) {
	t.Parallel()

	out, err := document.Write(document.Document{
		Title:   "Auswertung",
		Written: aMoment(),
		Sections: []document.Section{{
			Heading: "Einträge",
			Table: &document.Table{
				Columns: []string{"Beschreibung", "Stunden"},
				Numeric: []bool{false, true},
				Rows: [][]string{
					{strings.Repeat("ä", 4000), "1,0"},
					{"kurz", "2,0"},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("writing the document: %v", err)
	}

	if got := pages(out); got != 1 {
		t.Errorf("two rows became %d pages, so the long one was not contained", got)
	}
}

// The document is drawn in the colours the screen was.
//
// The chart in the middle of it is the installation's palette - it is a picture
// of the screen - and a page that surrounds it with somebody else's greys is
// two designs stapled together. It matters more on a dark screen: the chart
// comes out dark, and type that assumed white around it would be the wrong
// weight beside it.
//
// Asserted as a difference rather than as a colour. What ends up in the file is
// a PDF colour operator among compressed content, and finding one by hand is
// asserting on fpdf's output format; that two palettes produce two different
// files is the claim that matters, and it fails the moment the palette stops
// being read.
func TestTheDocumentIsWrittenInTheColoursItWasGiven(t *testing.T) {
	t.Parallel()

	same := func(colours document.Palette) []byte {
		out, err := document.Write(document.Document{
			Title:   "Auswertung",
			Written: aMoment(),
			Colours: colours,
			Sections: []document.Section{{Heading: "Stunden", Table: &document.Table{
				Columns: []string{"Zeitraum", "Stunden"},
				Numeric: []bool{false, true},
				Rows:    [][]string{{"August", "12,5"}},
			}}},
			Summary: []document.Line{{Label: "Gesamt", Value: "12,5"}},
		})
		if err != nil {
			t.Fatalf("writing the document: %v", err)
		}

		return out
	}

	light := same(document.Palette{
		Accent: "#2f6feb", Text: "#1c2126", Muted: "#626d78",
		Border: "#dfe3e8", Surface: "#ffffff",
	})

	dark := same(document.Palette{
		Accent: "#5b8dfa", Text: "#e8eaed", Muted: "#98a2ad",
		Border: "#2c333b", Surface: "#1c2126",
	})

	if bytes.Equal(light, dark) {
		t.Error("two palettes produced the same document, so the colours are not read")
	}

	// And a document given none is still a document rather than a refusal.
	plain := same(document.Palette{})

	if !bytes.HasPrefix(plain, []byte(pdfHeader)) {
		t.Error("a document with no palette is not a PDF")
	}

	if bytes.Equal(plain, light) {
		t.Error("the fallback shades and a real palette produce the same file, so " +
			"one of them is not being used")
	}
}

// A colour nobody can parse is not a reason to refuse a document.
//
// The palette is read off a live screen, and what a browser resolves a token to
// is not this package's to guarantee: a gradient, a colour function nobody has
// implemented here, an empty string from a token that does not exist. Each one
// falls back to the shade beside it and the page still reads.
func TestAnUnreadableColourFallsBackRatherThanFailing(t *testing.T) {
	t.Parallel()

	out, err := document.Write(document.Document{
		Title:   "Auswertung",
		Written: aMoment(),
		Colours: document.Palette{
			Accent:  "linear-gradient(red, blue)",
			Text:    "",
			Muted:   "rgb(1 2 3)",
			Border:  "#12",
			Surface: "#abc",
		},
		Summary: []document.Line{{Label: "Gesamt", Value: "1,0"}},
	})
	if err != nil {
		t.Fatalf("a palette full of nonsense was refused: %v", err)
	}

	if !bytes.HasPrefix(out, []byte(pdfHeader)) {
		t.Error("what came back is not a PDF")
	}
}
