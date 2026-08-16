package imaging

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

// Each place a logo is shown gets it at the size that place draws.
//
// One upload, three very different jobs: a mark beside a title, a banner over a
// sign-in card, and sixteen pixels in a browser tab. Handing the same few
// thousand pixels to all three is what this exists to stop - most visibly in the
// tab, where a browser given an image that size makes its own decision about
// whether to use it at all, and one that decides against shows nothing.
func TestALogoIsMadeInTheSizeEachPlaceDraws(t *testing.T) {
	logo := wordmark(t, 2776, 1299)

	for name, size := range map[string]struct{ width, height int }{
		"the header": {HeaderWidth, HeaderHeight},
		"the banner": {BannerWidth, BannerHeight},
	} {
		t.Run(name, func(t *testing.T) {
			out, err := Fit(logo, size.width, size.height)
			if err != nil {
				t.Fatalf("converting: %v", err)
			}

			img := decode(t, out)
			bounds := img.Bounds()

			if bounds.Dx() > size.width || bounds.Dy() > size.height {
				t.Errorf("%dx%d does not fit in %dx%d",
					bounds.Dx(), bounds.Dy(), size.width, size.height)
			}

			// Smaller than what was uploaded, which is the whole point: a wordmark
			// of a few hundred kilobytes was being sent to draw a 40px mark.
			if len(out) >= len(logo) {
				t.Errorf("the %s version is %d characters against the original's %d",
					name, len(out), len(logo))
			}

			if !drawnOn(img) {
				t.Error("nothing was drawn")
			}
		})
	}
}

// The tab's is square whatever shape the logo is; the other two are not padded.
//
// A tab icon that is not square gets one dimension stretched or the whole thing
// refused, depending on who is asked. A header places the mark itself, so a
// transparent margin baked into the image would only fight it for room.
func TestOnlyTheTabsVersionIsPadded(t *testing.T) {
	logo := wordmark(t, 2000, 500)

	icon := decode(t, mustFitIcon(t, logo))
	if icon.Bounds().Dx() != icon.Bounds().Dy() {
		t.Errorf("the tab's version is %dx%d, which is not square",
			icon.Bounds().Dx(), icon.Bounds().Dy())
	}

	header := decode(t, mustFit(t, logo, HeaderWidth, HeaderHeight))
	if header.Bounds().Dx() == header.Bounds().Dy() {
		t.Error("the header's version was padded into a square")
	}

	// Four to one in, four to one out: nothing is cropped anywhere.
	ratio := float64(header.Bounds().Dx()) / float64(header.Bounds().Dy())
	if ratio < 3.5 || ratio > 4.5 {
		t.Errorf("the header's version has a ratio of %.2f against the logo's 4", ratio)
	}
}

// A mark smaller than the box is left alone rather than blown up.
//
// Enlarging is the one thing scaling cannot do well: what comes out is the same
// picture, softer, in a larger file. The box is a ceiling.
func TestASmallMarkIsNotEnlarged(t *testing.T) {
	small := wordmark(t, 32, 32)

	icon := decode(t, mustFitIcon(t, small))

	// Padded to the square, but what was drawn into it is still 32.
	if icon.Bounds().Dx() != IconSize {
		t.Fatalf("the tab's version is %dpx across", icon.Bounds().Dx())
	}

	if drawn := drawnBounds(icon); drawn.Dx() > 32 {
		t.Errorf("a 32px mark was enlarged to %dpx", drawn.Dx())
	}
}

// Anything that is not one of the two formats a logo may be is refused.
func TestOnlyTheTwoFormatsAreConverted(t *testing.T) {
	for name, uri := range map[string]string{
		"an SVG":           "data:image/svg+xml;base64,PHN2Zy8+",
		"a page":           "data:text/html;base64,PGh0bWw+",
		"not encoded":      "data:image/png,plain",
		"not a URI at all": "hello",
	} {
		if _, err := FitIcon(uri); err == nil {
			t.Errorf("%s was converted", name)
		}
	}
}

func mustFitIcon(t *testing.T, logo string) string {
	t.Helper()

	out, err := FitIcon(logo)
	if err != nil {
		t.Fatalf("converting: %v", err)
	}

	return out
}

func mustFit(t *testing.T, logo string, width, height int) string {
	t.Helper()

	out, err := Fit(logo, width, height)
	if err != nil {
		t.Fatalf("converting: %v", err)
	}

	return out
}

func decode(t *testing.T, dataURI string) image.Image {
	t.Helper()

	_, encoded, _ := strings.Cut(dataURI, ",")

	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("the result is not base64: %v", err)
	}

	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("the result is not an image: %v", err)
	}

	return img
}

// wordmark is a logo shaped like the ones installations upload: a mark on the
// left, a band of text beside it, transparent everywhere else.
func wordmark(t *testing.T, width, height int) string {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))

	for y := range height {
		for x := range width {
			dx, dy := x-width/8, y-height/2

			switch {
			case dx*dx+dy*dy < (height/3)*(height/3):
				img.Set(x, y, color.RGBA{R: 205, G: 220, A: 255})
			case x > width/2 && y > height/3 && y < 2*height/3:
				img.Set(x, y, color.RGBA{A: 255})
			}
		}
	}

	var out bytes.Buffer

	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}

	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(out.Bytes())
}

func drawnOn(img image.Image) bool {
	return !drawnBounds(img).Empty()
}

// drawnBounds is the smallest rectangle holding everything that is not
// transparent.
func drawnBounds(img image.Image) image.Rectangle {
	bounds := img.Bounds()
	found := image.Rectangle{Min: bounds.Max, Max: bounds.Min}

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if _, _, _, alpha := img.At(x, y).RGBA(); alpha == 0 {
				continue
			}

			found.Min.X = min(found.Min.X, x)
			found.Min.Y = min(found.Min.Y, y)
			found.Max.X = max(found.Max.X, x+1)
			found.Max.Y = max(found.Max.Y, y+1)
		}
	}

	if found.Min.X > found.Max.X {
		return image.Rectangle{}
	}

	return found
}
