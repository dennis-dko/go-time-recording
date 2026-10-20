package imaging_test

import (
	"bytes"
	"image/png"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/support/imaging"
)

// The mark is drawn at the size asked for, and is the mark: a blue tile with the
// white glass knocked into it, and transparent outside the tile's corners.
func TestTheShippedMarkIsDrawnAtTheSizeAsked(t *testing.T) {
	for _, size := range []int{16, 192, 512} {
		encoded, err := imaging.MarkPNG(size)
		if err != nil {
			t.Fatalf("drawing the mark at %d: %v", size, err)
		}

		img, err := png.Decode(bytes.NewReader(encoded))
		if err != nil {
			t.Fatalf("the mark at %d is not a PNG: %v", size, err)
		}

		if b := img.Bounds(); b.Dx() != size || b.Dy() != size {
			t.Fatalf("the mark asked for at %d is %dx%d", size, b.Dx(), b.Dy())
		}

		// The very corner lies outside the rounded tile.
		if _, _, _, a := img.At(0, 0).RGBA(); a != 0 {
			t.Errorf("the corner of the %d mark is not transparent", size)
		}

		if size < 64 {
			continue
		}

		// Between the glass and the tile's edge, a little in from the left at mid
		// height, the tile shows: blue, and opaque.
		r, g, b, a := img.At(size*5/32, size/2).RGBA()
		if a>>8 != 0xff || b>>8 < 0xd0 || r>>8 > 0x60 || g>>8 > 0x90 {
			t.Errorf("the %d mark's tile reads as rgba(%d,%d,%d,%d), want the accent blue",
				size, r>>8, g>>8, b>>8, a>>8)
		}

		// The upper glass at its widest is white.
		if r, g, b, _ := img.At(size/2, size*9/32).RGBA(); r>>8 < 0xf0 || g>>8 < 0xf0 || b>>8 < 0xf0 {
			t.Errorf("the %d mark's glass reads as rgb(%d,%d,%d), want white", size, r>>8, g>>8, b>>8)
		}
	}
}
