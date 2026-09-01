package document

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

// A chart inside the byte limit can still be enormous once it is read.
//
// The endpoint bounds the picture at 1 MB, and until this was added that was the
// only bound. A PNG compresses by orders of magnitude, and the browser sends
// exactly the kind that costs most to read: a canvas produces colour type 6, so
// the alpha channel has to be split from the colour, and splitting it means
// inflating the whole image.
//
// Measured, not estimated, through Write with a flat RGBA chart:
//
//	 2000 x  2000 →  20 KB on the wire →   58 MB allocated
//	 8000 x  8000 → 260 KB on the wire →  782 MB allocated
//	12000 x 12000 → 566 KB on the wire → 2643 MB allocated
//
// All three are inside the 1 MB limit. Extrapolated to it, about 16000 square,
// which is roughly 5 GB - on a machine whose disk is an SD card and whose memory
// is measured in single-figure gigabytes.
//
// The dimensions were already being read here: writeChart calls png.DecodeConfig
// and checks that they are positive. There was simply no upper bound beside the
// lower one.
func TestAChartWithTooManyPixelsIsRefusedBeforeItIsRead(t *testing.T) {
	// Comfortably inside the byte limit and far outside the pixel one.
	huge := flatPNG(t, 8000, 8000)

	t.Logf("the crafted chart is %.0f KB, against a 1 MB limit", float64(len(huge))/1024)

	if len(huge) >= 1<<20 {
		t.Fatalf("the crafted chart is %d bytes and the endpoint would refuse it on "+
			"size alone; this case has to get past that to mean anything", len(huge))
	}

	_, err := Write(Document{
		Title:    "Report",
		Sections: []Section{{Heading: "Chart", Chart: huge}},
	})

	if err == nil {
		t.Fatal("a 64-megapixel chart was placed rather than refused")
	}

	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("refused for the wrong reason: %v", err)
	}
}

// And an ordinary chart is still placed.
//
// The largest a real one gets is the chart card's own box times CHART_SCALE,
// which app.js sets to 2 - so a few million pixels even on a very wide screen.
// This is that shape, and the bound must not be satisfied by refusing it.
func TestAnOrdinaryChartIsStillPlaced(t *testing.T) {
	ordinary := flatPNG(t, 1600, 600)

	out, err := Write(Document{
		Title:    "Report",
		Sections: []Section{{Heading: "Chart", Chart: ordinary}},
	})
	if err != nil {
		t.Fatalf("an ordinary chart was refused: %v", err)
	}

	if len(out) == 0 {
		t.Error("the document came back empty")
	}
}

// flatPNG encodes a single-colour RGBA image without ever holding one.
//
// Not opaque, deliberately: an opaque image is written as colour type 2, which a
// reader can pass through without inflating, and the case would then measure the
// cheap path rather than the one a browser canvas actually produces.
func flatPNG(t *testing.T, width, height int) []byte {
	t.Helper()

	buf := new(bytes.Buffer)
	encoder := png.Encoder{CompressionLevel: png.BestCompression}

	if err := encoder.Encode(buf, flatChart{w: width, h: height}); err != nil {
		t.Fatalf("encoding a %dx%d chart: %v", width, height, err)
	}

	return buf.Bytes()
}

type flatChart struct{ w, h int }

func (f flatChart) ColorModel() color.Model { return color.RGBAModel }
func (f flatChart) Bounds() image.Rectangle { return image.Rect(0, 0, f.w, f.h) }
func (f flatChart) At(int, int) color.Color {
	return color.RGBA{R: 10, G: 120, B: 200, A: 254}
}
