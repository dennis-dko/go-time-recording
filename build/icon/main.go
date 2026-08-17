// Command icon draws the shipped mark and writes it as build/icon.ico.
//
// The .exe gets its icon from that file, by way of goversioninfo and
// build/windows-resource.sh. It is a binary in the tree, which is a copy of a
// picture that also exists as internal/interface/web/assets/favicon.svg - and a
// copy nobody can regenerate is a copy that silently keeps the old mark forever.
// That is exactly what happened to the tab icon: it was cached at an address
// that could not change, and went on showing a picture nobody had chosen. This
// is the answer for the Windows side of the same problem.
//
// The geometry below is the same as favicon.svg's, in the same 32-unit box.
// Changing one means changing the other; there is no renderer here that could
// read the SVG, and adding one to rasterise nine paths would be a dependency
// that does more than this needs.
//
//	go run ./build/icon > build/icon.ico
//
// Four sizes, because Windows asks for four: 16 in the Explorer's detail view,
// 32 in the list and on the taskbar, 48 medium, 256 in the file's properties and
// wherever a preview is large. Each is drawn at its own size rather than scaled
// from one big one - a 256 shrunk to 16 loses the corners of the tile.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"

	"golang.org/x/image/vector"
)

// The mark, in the 32-unit box favicon.svg uses.
const (
	box = 32.0

	tileX, tileY, tileSide, tileRadius = 3.0, 3.0, 26.0, 7.6

	glassLeft, glassRight = 9.6, 22.4
	glassTop, glassBottom = 8.6, 23.4
	glassWaist            = 16.0
)

// The two inks. The lower half of the glass is the sand that has already
// fallen, so it is the same white held back to a little over half.
var (
	tile       = color.NRGBA{R: 0x2f, G: 0x6f, B: 0xeb, A: 0xff}
	sand       = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	fallenTo   = 0.55
	icoSizes   = []int{16, 32, 48, 256}
	cubicKappa = float32(0.55228)
)

func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "drawing the icon:", err)
		os.Exit(1)
	}
}

func run(out *os.File) error {
	frames := make([][]byte, 0, len(icoSizes))

	for _, size := range icoSizes {
		var encoded bytes.Buffer

		if err := png.Encode(&encoded, draw(size)); err != nil {
			return fmt.Errorf("encoding %dpx: %w", size, err)
		}

		frames = append(frames, encoded.Bytes())
	}

	return writeICO(out, icoSizes, frames)
}

// draw renders the mark at one size.
func draw(size int) image.Image {
	scale := float32(size) / box

	tileMask := fill(size, func(r *vector.Rasterizer) {
		roundedRect(r, tileX*scale, tileY*scale, tileSide*scale, tileSide*scale, tileRadius*scale)
	})

	upper := fill(size, func(r *vector.Rasterizer) {
		triangle(r, scale, glassTop)
	})

	lower := fill(size, func(r *vector.Rasterizer) {
		triangle(r, scale, glassBottom)
	})

	out := image.NewNRGBA(image.Rect(0, 0, size, size))

	for y := range size {
		for x := range size {
			at := image.Pt(x, y)

			covered := float64(tileMask.AlphaAt(x, y).A) / 255
			if covered == 0 {
				continue
			}

			// The tile carries the colour; the glass is knocked into it. Both
			// halves are the same white, the lower one held back - so the two are
			// one shape read as an hourglass rather than two separate marks.
			shade := blend(tile, sand, float64(upper.AlphaAt(x, y).A)/255)
			shade = blend(shade, sand, float64(lower.AlphaAt(x, y).A)/255*fallenTo)
			shade.A = uint8(covered*255 + 0.5)

			out.SetNRGBA(at.X, at.Y, shade)
		}
	}

	return out
}

// fill rasterises one path into a coverage mask, anti-aliased.
func fill(size int, path func(*vector.Rasterizer)) *image.Alpha {
	r := vector.NewRasterizer(size, size)
	path(r)

	mask := image.NewAlpha(image.Rect(0, 0, size, size))
	r.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})

	return mask
}

// triangle is one half of the glass: a horizontal edge and the point it meets in
// the middle. Which half is decided by where the edge is.
func triangle(r *vector.Rasterizer, scale float32, edge float64) {
	r.MoveTo(float32(glassLeft)*scale, float32(edge)*scale)
	r.LineTo(float32(glassRight)*scale, float32(edge)*scale)
	r.LineTo(float32(glassWaist)*scale, float32(glassWaist)*scale)
	r.ClosePath()
}

// roundedRect traces the tile. The corners are cubics rather than arcs, which
// the rasteriser has no notion of; the constant is the usual approximation of a
// quarter circle and is exact enough at 256 pixels, let alone 16.
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

// writeICO packs the frames into the container Windows reads.
//
// PNG inside rather than the older bitmap-with-mask: every Windows that this
// application supports reads it, and the 256 as a bitmap would be a quarter of a
// megabyte on its own.
func writeICO(out *os.File, sizes []int, frames [][]byte) error {
	var buf bytes.Buffer

	// ICONDIR: reserved, type 1 (icon), how many.
	_ = binary.Write(&buf, binary.LittleEndian, uint16(0))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(len(frames)))

	const dirEntry = 16

	offset := 6 + dirEntry*len(frames)

	for i, frame := range frames {
		// 256 is written as 0: the field is one byte, and 256 does not fit in it.
		side := byte(sizes[i])
		if sizes[i] >= 256 {
			side = 0
		}

		buf.WriteByte(side)                                     // width
		buf.WriteByte(side)                                     // height
		buf.WriteByte(0)                                        // colours in the palette; none, this is truecolour
		buf.WriteByte(0)                                        // reserved
		_ = binary.Write(&buf, binary.LittleEndian, uint16(1))  // colour planes
		_ = binary.Write(&buf, binary.LittleEndian, uint16(32)) // bits per pixel
		_ = binary.Write(&buf, binary.LittleEndian, uint32(len(frame)))
		_ = binary.Write(&buf, binary.LittleEndian, uint32(offset))

		offset += len(frame)
	}

	for _, frame := range frames {
		buf.Write(frame)
	}

	_, err := out.Write(buf.Bytes())

	return err
}
