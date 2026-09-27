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
			out, err := Fit(logo, Crop{}, size.width, size.height)
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
		if _, err := FitIcon(uri, Crop{}); err == nil {
			t.Errorf("%s was converted", name)
		}
	}
}

func mustFitIcon(t *testing.T, logo string) string {
	t.Helper()

	out, err := FitIcon(logo, Crop{})
	if err != nil {
		t.Fatalf("converting: %v", err)
	}

	return out
}

func mustFit(t *testing.T, logo string, width, height int) string {
	t.Helper()

	out, err := Fit(logo, Crop{}, width, height)
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

// A part of the logo can be chosen for each place it is shown.
//
// A wordmark that reads as a banner is a smear in a tab, and the part worth
// keeping there is usually the mark at one end rather than whatever falls in the
// middle. Nobody can guess which, so it is chosen - and per place, because the
// answer differs between a wide header and a square tab.
func TestOnlyTheChosenPartIsUsed(t *testing.T) {
	// Left half green, right half black, so which half came through is a question
	// about a colour rather than about a size.
	logo := halves(t, 400, 200)

	left, err := Fit(logo, Crop{X: 0, Y: 0, W: 0.5, H: 1}, 100, 100)
	if err != nil {
		t.Fatalf("cropping to the left: %v", err)
	}

	if got := middleColour(t, left); got != (colours{green: true}) {
		t.Errorf("the left half came out as %+v", got)
	}

	right, err := Fit(logo, Crop{X: 0.5, Y: 0, W: 0.5, H: 1}, 100, 100)
	if err != nil {
		t.Fatalf("cropping to the right: %v", err)
	}

	if got := middleColour(t, right); got != (colours{black: true}) {
		t.Errorf("the right half came out as %+v", got)
	}
}

// A crop that makes no sense is the whole image rather than nothing.
//
// It arrives from a browser, so it can be anything: a stray drag, a rounding
// error on a small image, a value somebody typed into the request by hand. A logo
// shown complete is never wrong, only sometimes small - and nothing here should
// be able to produce an empty tab, which is the failure this all started with.
func TestAnImpossibleCropIsTheWholeImage(t *testing.T) {
	logo := halves(t, 400, 200)

	whole := decode(t, mustFit(t, logo, 100, 100))

	for name, crop := range map[string]Crop{
		"nothing at all":    {X: 0.5, Y: 0.5, W: 0, H: 0},
		"outside the image": {X: 4, Y: 4, W: 1, H: 1},
		"negative":          {X: -1, Y: -1, W: -1, H: -1},
		"vanishingly small": {X: 0.5, Y: 0.5, W: 0.0000001, H: 0.0000001},
	} {
		t.Run(name, func(t *testing.T) {
			out, err := Fit(logo, crop, 100, 100)
			if err != nil {
				t.Fatalf("converting: %v", err)
			}

			got := decode(t, out)

			if got.Bounds() != whole.Bounds() {
				t.Errorf("%s produced %v rather than the whole image %v",
					name, got.Bounds(), whole.Bounds())
			}
		})
	}
}

type colours struct{ green, black bool }

// middleColour is what is in the middle of a converted image, which is what says
// which part of the original it was made from.
func middleColour(t *testing.T, dataURI string) colours {
	t.Helper()

	img := decode(t, dataURI)
	bounds := img.Bounds()

	r, g, b, _ := img.At(bounds.Min.X+bounds.Dx()/2, bounds.Min.Y+bounds.Dy()/2).RGBA()

	return colours{
		green: g > r && g > b,
		black: r < 0x4000 && g < 0x4000 && b < 0x4000,
	}
}

// halves is an image whose left half is green and right half is black.
func halves(t *testing.T, width, height int) string {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))

	for y := range height {
		for x := range width {
			if x < width/2 {
				img.Set(x, y, color.RGBA{G: 220, A: 255})
			} else {
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

// A part chosen in a shape the place does not have is fitted into it, not
// stretched to fill it.
//
// The part is chosen freely - any corner, any shape - so a square can be picked
// for the header and a strip for the tab, and neither has the proportions of the
// box it is going into. Filling the box would mean distorting the logo, which is
// the one thing an instance's mark must never come out as. So it is scaled until
// it fits and left at its own proportions, and the sides that are left over are
// simply not there for the header and padded for the tab.
func TestAPartOfAnyShapeIsFittedRatherThanStretched(t *testing.T) {
	logo := wordmark(t, 2000, 500)

	// A square out of a four-to-one wordmark, headed for a five-and-a-half-to-one
	// header box.
	square := Crop{X: 0.4, Y: 0, W: 0.25, H: 1}

	header := decode(t, mustFitCrop(t, logo, square, HeaderWidth, HeaderHeight))

	if ratio := float64(header.Bounds().Dx()) / float64(header.Bounds().Dy()); ratio < 0.9 || ratio > 1.1 {
		t.Errorf("a square part came out of the header's box at %dx%d, a ratio of %.2f",
			header.Bounds().Dx(), header.Bounds().Dy(), ratio)
	}

	if header.Bounds().Dy() > HeaderHeight {
		t.Errorf("the header's version is %dpx tall, taller than the %dpx it is given",
			header.Bounds().Dy(), HeaderHeight)
	}

	// And the other way round: a wide strip headed for the square tab.
	strip := Crop{X: 0, Y: 0.4, W: 1, H: 0.2}

	icon := decode(t, mustFitIconCrop(t, logo, strip))

	if icon.Bounds().Dx() != IconSize || icon.Bounds().Dy() != IconSize {
		t.Fatalf("the tab's version is %dx%d rather than %dpx square",
			icon.Bounds().Dx(), icon.Bounds().Dy(), IconSize)
	}

	// Padded into the square rather than squashed to it: what was drawn is still
	// the twenty-to-one strip that was chosen.
	drawn := drawnBounds(icon)

	if ratio := float64(drawn.Dx()) / float64(drawn.Dy()); ratio < 5 {
		t.Errorf("a twenty-to-one strip was drawn into the tab at %dx%d, a ratio of %.2f",
			drawn.Dx(), drawn.Dy(), ratio)
	}
}

func mustFitCrop(t *testing.T, logo string, crop Crop, width, height int) string {
	t.Helper()

	out, err := Fit(logo, crop, width, height)
	if err != nil {
		t.Fatalf("converting: %v", err)
	}

	return out
}

func mustFitIconCrop(t *testing.T, logo string, crop Crop) string {
	t.Helper()

	out, err := FitIcon(logo, crop)
	if err != nil {
		t.Fatalf("converting: %v", err)
	}

	return out
}
