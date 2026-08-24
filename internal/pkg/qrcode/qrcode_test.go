package qrcode_test

import (
	"encoding/base64"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"rsc.io/qr"

	"github.com/dennis-dko/go-time-recording/internal/pkg/qrcode"
)

// A QR code is only ever read by a machine, so the usual "does it look right" is
// no help at all: a grid drawn one module off, or with x and y swapped, renders a
// perfectly plausible pattern that no phone can read.
//
// These tests therefore check the drawing against the code it is supposed to be
// drawing - every module, both ways - and then check the result has the structure
// a scanner looks for, which is what would still be wrong if the encoder itself
// were ever swapped for another.

// modulePattern matches the "M<x> <y>h1v1h-1z" of one module.
var modulePattern = regexp.MustCompile(`M(\d+) (\d+)h1v1h-1z`)

const sampleURI = "otpauth://totp/Time%20Recording:hanne@example.com" +
	"?secret=JBSWY3DPEHPK3PXP&issuer=Time+Recording&digits=6&period=30"

// modules parses the drawn squares back out of the path.
func modules(t *testing.T, svg string) map[[2]int]bool {
	t.Helper()

	found := map[[2]int]bool{}

	for _, match := range modulePattern.FindAllStringSubmatch(svg, -1) {
		x, xErr := strconv.Atoi(match[1])
		y, yErr := strconv.Atoi(match[2])

		if xErr != nil || yErr != nil {
			t.Fatalf("unreadable module coordinates %q, %q", match[1], match[2])
		}

		found[[2]int{x, y}] = true
	}

	if len(found) == 0 {
		t.Fatalf("the path contains no modules:\n%s", svg)
	}

	return found
}

// The drawing is the code, module for module. A single missing or extra square is
// a code that will not scan, and nothing about the picture would say so.
func TestEveryModuleOfTheCodeIsDrawnAndNoOthers(t *testing.T) {
	svg, err := qrcode.SVG(sampleURI)
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}

	code, err := qr.Encode(sampleURI, qr.M)
	if err != nil {
		t.Fatalf("encoding the reference: %v", err)
	}

	drawn := modules(t, svg)

	// The margin is part of the coordinates, so the comparison has to shift by it.
	const quietZone = 4

	var missing, extra int

	for y := 0; y < code.Size; y++ {
		for x := 0; x < code.Size; x++ {
			isDrawn := drawn[[2]int{x + quietZone, y + quietZone}]

			switch {
			case code.Black(x, y) && !isDrawn:
				missing++
			case !code.Black(x, y) && isDrawn:
				extra++
			}
		}
	}

	if missing > 0 || extra > 0 {
		t.Errorf("the drawing differs from the code: %d module(s) missing, %d extra",
			missing, extra)
	}

	// And nothing outside the code at all, which is what an off-by-one in the
	// margin would produce.
	for at := range drawn {
		if at[0] < quietZone || at[1] < quietZone ||
			at[0] >= code.Size+quietZone || at[1] >= code.Size+quietZone {
			t.Errorf("a module is drawn outside the code at %v", at)
		}
	}
}

// The three finder patterns, which are the first thing a scanner looks for: a
// 7x7 ring of black around a white ring around a 3x3 black centre. Checked
// independently of the encoder, so swapping it out cannot silently produce
// something unscannable.
func TestTheFinderPatternsAreWhereAScannerLooks(t *testing.T) {
	svg, err := qrcode.SVG(sampleURI)
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}

	drawn := modules(t, svg)
	side := sideOf(t, svg)

	const quietZone = 4

	size := side - 2*quietZone

	// Top-left, top-right, bottom-left. There is deliberately none at
	// bottom-right; that is how a scanner tells which way up the code is.
	corners := [][2]int{
		{quietZone, quietZone},
		{quietZone + size - 7, quietZone},
		{quietZone, quietZone + size - 7},
	}

	for _, corner := range corners {
		for dy := range 7 {
			for dx := range 7 {
				at := [2]int{corner[0] + dx, corner[1] + dy}

				// The outer ring is black, the ring inside it white, the 3x3
				// centre black.
				edge := dx == 0 || dy == 0 || dx == 6 || dy == 6
				inner := dx >= 2 && dx <= 4 && dy >= 2 && dy <= 4
				want := edge || inner

				if drawn[at] != want {
					t.Errorf("finder pattern at %v: module %d,%d is %v, want %v",
						corner, dx, dy, drawn[at], want)
				}
			}
		}
	}
}

// sideOf reads the module count back out of the viewBox.
func sideOf(t *testing.T, svg string) int {
	t.Helper()

	match := regexp.MustCompile(`viewBox="0 0 (\d+) (\d+)"`).FindStringSubmatch(svg)
	if match == nil {
		t.Fatalf("no viewBox in:\n%s", svg)
	}

	if match[1] != match[2] {
		t.Errorf("the code is %sx%s; a QR code is square", match[1], match[2])
	}

	side, err := strconv.Atoi(match[1])
	if err != nil {
		t.Fatalf("unreadable viewBox: %v", err)
	}

	return side
}

// The margin has to be drawn light rather than left transparent: on the dark theme
// a transparent margin is dark, and a scanner needs light around the code.
func TestTheQuietZoneIsDrawnAndClear(t *testing.T) {
	svg, err := qrcode.SVG(sampleURI)
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}

	if !strings.Contains(svg, `fill="#ffffff"`) {
		t.Error("the background is not painted, so the margin follows the theme")
	}

	drawn := modules(t, svg)
	side := sideOf(t, svg)

	const quietZone = 4

	for at := range drawn {
		nearEdge := at[0] < quietZone || at[1] < quietZone ||
			at[0] >= side-quietZone || at[1] >= side-quietZone
		if nearEdge {
			t.Errorf("a module at %v is inside the four-module margin", at)
		}
	}
}

// A code that scales: no width or height, so the interface sizes it.
func TestTheCodeScalesWithItsContainer(t *testing.T) {
	svg, err := qrcode.SVG(sampleURI)
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}

	// The opening tag only: the background rect fills its parent with a
	// percentage, which is the opposite of pinning a size.
	open := svg[:strings.Index(svg, ">")+1]

	for _, fixed := range []string{" width=", " height="} {
		if strings.Contains(open, fixed) {
			t.Errorf("the svg element pins its%s, so it cannot be sized on screen", fixed)
		}
	}

	if !strings.Contains(svg, `shape-rendering="crispEdges"`) {
		t.Error("without crispEdges the modules are drawn with soft borders")
	}
}

// The data URI is what reaches an img src.
func TestTheDataURICarriesTheSameSVG(t *testing.T) {
	uri, err := qrcode.SVGDataURI(sampleURI)
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}

	const prefix = "data:image/svg+xml;base64,"

	if !strings.HasPrefix(uri, prefix) {
		t.Fatalf("the data URI does not declare an SVG: %.40s", uri)
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(uri, prefix))
	if err != nil {
		t.Fatalf("the payload is not base64: %v", err)
	}

	svg, err := qrcode.SVG(sampleURI)
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}

	if string(decoded) != svg {
		t.Error("the data URI does not carry the rendered code")
	}
}

// Nothing to encode is a caller's mistake, not an empty picture.
func TestAnEmptyStringIsRefused(t *testing.T) {
	if _, err := qrcode.SVG(""); err == nil {
		t.Error("an empty string produced a code")
	}

	if _, err := qrcode.SVGDataURI(""); err == nil {
		t.Error("an empty string produced a data URI")
	}
}

// A longer URI needs a bigger code, which is the encoder choosing a version. Worth
// pinning: a fixed version would silently truncate a long issuer or address.
func TestALongerURIGrowsTheCode(t *testing.T) {
	short, err := qrcode.SVG("otpauth://totp/a?secret=JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatalf("rendering the short one: %v", err)
	}

	long, err := qrcode.SVG(sampleURI + "&issuer=" + strings.Repeat("x", 200))
	if err != nil {
		t.Fatalf("rendering the long one: %v", err)
	}

	if sideOf(t, long) <= sideOf(t, short) {
		t.Errorf("a %d-module code holds 200 more characters than a %d-module one",
			sideOf(t, long), sideOf(t, short))
	}
}
