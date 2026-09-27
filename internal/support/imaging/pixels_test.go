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

// A small file is not a small image.
//
// The logo is bounded by the bytes it arrives as - 256 KB - and nothing bounded
// what those bytes decode to. A PNG of one flat colour compresses at about
// 1250:1, so 8000 by 8000 encodes to 199 KB, which is inside that limit, and
// decodes to 244 MB. Measured, not estimated. Saving the appearance then decodes
// the stored logo three times, once per derivative.
//
// image.DecodeConfig reads the header and answers the dimensions without
// decoding anything - it took no measurable time on the same file - so the bound
// costs nothing to apply.
//
// The right needed is settings:manage, so this is an administrator rather than
// anybody; that makes it a smaller blast radius and not a different kind of
// problem, because an installation that hands administration to a person has
// handed them this.
func TestAnImageWithTooManyPixelsIsRefusedBeforeItIsDecoded(t *testing.T) {
	huge := solidPNG(t, 8000, 8000)

	t.Logf("the crafted logo is %.0f KB", float64(len(huge))/1024)

	if len(huge) >= 256*1024 {
		t.Fatalf("the crafted logo is %d bytes and would be refused on size alone; "+
			"this case has to get past that to mean anything", len(huge))
	}

	uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString(huge)

	if _, err := Fit(uri, Crop{}, HeaderWidth, HeaderHeight); err == nil {
		t.Error("a 64-megapixel logo was scaled rather than refused")
	} else if !strings.Contains(err.Error(), "too large") {
		t.Errorf("refused for the wrong reason: %v", err)
	}

	if _, err := FitIcon(uri, Crop{}); err == nil {
		t.Error("a 64-megapixel logo was accepted as a tab icon")
	}

	if _, err := ToIcon(huge); err == nil {
		t.Error("a 64-megapixel logo was accepted by the icon endpoint")
	}
}

// And an ordinary logo is still scaled, so the bound is not refusing everything.
func TestAnOrdinaryLogoIsStillScaled(t *testing.T) {
	ordinary := solidPNG(t, 600, 200)
	uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString(ordinary)

	out, err := Fit(uri, Crop{}, HeaderWidth, HeaderHeight)
	if err != nil {
		t.Fatalf("an ordinary logo was refused: %v", err)
	}

	if !strings.HasPrefix(out, "data:image/png;base64,") {
		t.Errorf("the scaled logo is not a PNG data URI: %.40s", out)
	}
}

// solidPNG encodes a single-colour image without ever holding one.
//
// The point of the case is that a large image can be small on the wire; building
// it the ordinary way would allocate exactly the memory the bound exists to
// prevent.
func solidPNG(t *testing.T, width, height int) []byte {
	t.Helper()

	buf := new(bytes.Buffer)
	encoder := png.Encoder{CompressionLevel: png.BestCompression}

	if err := encoder.Encode(buf, flat{w: width, h: height}); err != nil {
		t.Fatalf("encoding a %dx%d image: %v", width, height, err)
	}

	return buf.Bytes()
}

type flat struct{ w, h int }

func (f flat) ColorModel() color.Model { return color.RGBAModel }
func (f flat) Bounds() image.Rectangle { return image.Rect(0, 0, f.w, f.h) }
func (f flat) At(int, int) color.Color { return color.RGBA{R: 40, G: 90, B: 200, A: 255} }
