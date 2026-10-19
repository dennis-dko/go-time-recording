package imaging

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"

	"golang.org/x/image/vector"
)

// The mark, in the 32-unit box favicon.svg uses.
//
// Drawn rather than kept as pictures, because a picture in the tree is a copy of
// favicon.svg nobody can regenerate - and a copy that silently keeps the old mark
// is what happened to the tab icon once already. The geometry is the SVG's;
// changing one means changing the other. There is no renderer here that could read
// the SVG, and one added to rasterise nine paths would be a dependency doing far
// more than this needs.
const (
	markBox = 32.0

	tileX, tileY, tileSide, tileRadius = 3.0, 3.0, 26.0, 7.6

	glassLeft, glassRight = 9.6, 22.4
	glassTop, glassBottom = 8.6, 23.4
	glassWaist            = 16.0
)

// The two inks. The lower half of the glass is the sand that has already fallen,
// so it is the same white held back to a little over half.
var (
	tileInk    = color.NRGBA{R: 0x2f, G: 0x6f, B: 0xeb, A: 0xff}
	sandInk    = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	fallenTo   = 0.55
	cubicKappa = float32(0.55228)
)

// Mark draws the hourglass this application ships with, size pixels square.
//
// Each size is drawn at that size rather than scaled from a larger one: a 256
// shrunk to 16 loses the corners of the tile, and a 64 blown up to 512 is a blur
// on exactly the home screen it was made for.
func Mark(size int) image.Image {
	scale := float32(size) / markBox

	tileMask := coverage(size, func(r *vector.Rasterizer) {
		roundedRect(r, tileX*scale, tileY*scale, tileSide*scale, tileSide*scale, tileRadius*scale)
	})

	upper := coverage(size, func(r *vector.Rasterizer) {
		glassHalf(r, scale, glassTop)
	})

	lower := coverage(size, func(r *vector.Rasterizer) {
		glassHalf(r, scale, glassBottom)
	})

	out := image.NewNRGBA(image.Rect(0, 0, size, size))

	for y := range size {
		for x := range size {
			covered := float64(tileMask.AlphaAt(x, y).A) / 255
			if covered == 0 {
				continue
			}

			// The tile carries the colour; the glass is knocked into it. Both halves
			// are the same white, the lower one held back - so the two are one shape
			// read as an hourglass rather than two separate marks.
			shade := blend(tileInk, sandInk, float64(upper.AlphaAt(x, y).A)/255)
			shade = blend(shade, sandInk, float64(lower.AlphaAt(x, y).A)/255*fallenTo)
			shade.A = uint8(covered*255 + 0.5)

			out.SetNRGBA(x, y, shade)
		}
	}

	return out
}

// MarkPNG is Mark, encoded.
func MarkPNG(size int) ([]byte, error) {
	var encoded bytes.Buffer

	if err := png.Encode(&encoded, Mark(size)); err != nil {
		return nil, fmt.Errorf("encoding the mark at %dpx: %w", size, err)
	}

	return encoded.Bytes(), nil
}

// coverage rasterises one path into a mask, anti-aliased.
func coverage(size int, path func(*vector.Rasterizer)) *image.Alpha {
	r := vector.NewRasterizer(size, size)
	path(r)

	mask := image.NewAlpha(image.Rect(0, 0, size, size))
	r.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})

	return mask
}

// glassHalf is one half of the glass: a horizontal edge and the point it meets in
// the middle. Which half is decided by where the edge is.
func glassHalf(r *vector.Rasterizer, scale float32, edge float64) {
	r.MoveTo(float32(glassLeft)*scale, float32(edge)*scale)
	r.LineTo(float32(glassRight)*scale, float32(edge)*scale)
	r.LineTo(float32(glassWaist)*scale, float32(glassWaist)*scale)
	r.ClosePath()
}

// roundedRect traces the tile. The corners are cubics rather than arcs, which the
// rasteriser has no notion of; the constant is the usual approximation of a
// quarter circle and is exact enough at 512 pixels, let alone 16.
func roundedRect(r *vector.Rasterizer, x, y, w, h, radius float32) {
	k := radius * cubicKappa

	r.MoveTo(x+radius, y)
	r.LineTo(x+w-radius, y)
	r.CubeTo(x+w-radius+k, y, x+w, y+radius-k, x+w, y+radius)
	r.LineTo(x+w, y+h-radius)
	r.CubeTo(x+w, y+h-radius+k, x+w-radius+k, y+h, x+w-radius, y+h)
	r.LineTo(x+radius, y+h)
	r.CubeTo(x+radius-k, y+h, x, y+h-radius+k, x, y+h-radius)
	r.LineTo(x, y+radius)
	r.CubeTo(x, y+radius-k, x+radius-k, y, x+radius, y)
	r.ClosePath()
}

// blend mixes towards a colour by a fraction of it.
func blend(base, over color.NRGBA, amount float64) color.NRGBA {
	mix := func(a, b uint8) uint8 {
		return uint8(float64(a)*(1-amount) + float64(b)*amount + 0.5)
	}

	return color.NRGBA{R: mix(base.R, over.R), G: mix(base.G, over.G), B: mix(base.B, over.B), A: base.A}
}
