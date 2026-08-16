package web

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"sync"

	// Registered for image.Decode. These two and no others, which is what a logo
	// may be.
	_ "image/jpeg"
	_ "image/png"

	xdraw "golang.org/x/image/draw"
)

// iconSize is what a converted logo is served at.
//
// 64 rather than 16: a tab draws 16, and a browser on a high-density screen
// draws 32 or more for the same tab, then again for a bookmark and a pinned
// shortcut. Serving the smallest of those makes every other one a blur.
//
// And not larger. The point of converting at all is that what leaves here is
// small, square and ordinary, so nothing downstream has to make a decision about
// it.
const iconSize = 64

// toIcon turns a stored logo into a square PNG.
//
// The logo was served exactly as it was stored, which is where the tab icon kept
// going wrong. What an installation uploads is a wordmark: a few thousand pixels
// across, twice as wide as it is tall, made for a header. Handing that to a
// browser as a tab icon leaves every decision to the browser - whether to accept
// it at that size at all, how to fit two-to-one into a square, whether to bother.
// The answers differ by engine, and a browser that decides not to shows nothing
// and says nothing.
//
// So the decision is made here, once, and what is served is the thing every
// engine handles identically.
//
// Fitted rather than cropped. Cropping a wordmark to a square keeps whichever
// part happens to be in the middle, which for most logos is the middle of a word;
// fitting keeps all of it and is what the preview beside the upload shows. A
// wordmark is a smudge at sixteen pixels either way - that is a fact about
// wordmarks, and the preview is there so it is discovered before saving rather
// than in the tab afterwards.
func toIcon(body []byte) ([]byte, error) {
	source, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("the logo could not be read as an image: %w", err)
	}

	bounds := source.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return nil, fmt.Errorf("the logo has no size")
	}

	// The largest fit inside the square, keeping the proportions.
	scale := min(
		float64(iconSize)/float64(bounds.Dx()),
		float64(iconSize)/float64(bounds.Dy()),
	)

	width := max(1, int(float64(bounds.Dx())*scale))
	height := max(1, int(float64(bounds.Dy())*scale))

	// Transparent, so a logo that is not square keeps its shape against whatever
	// the browser draws behind it - a tab strip is light in one theme and dark in
	// the other, and a white box would be wrong in one of them.
	icon := image.NewRGBA(image.Rect(0, 0, iconSize, iconSize))

	at := image.Rect(
		(iconSize-width)/2, (iconSize-height)/2,
		(iconSize-width)/2+width, (iconSize-height)/2+height,
	)

	// CatmullRom because this is almost always a large reduction - a hundred
	// source pixels to one - and the cheap samplers turn thin strokes into gaps at
	// that ratio, which is exactly what a wordmark is made of.
	xdraw.CatmullRom.Scale(icon, at, source, bounds, draw.Over, nil)

	var out bytes.Buffer

	if err := png.Encode(&out, icon); err != nil {
		return nil, fmt.Errorf("the icon could not be written: %w", err)
	}

	return out.Bytes(), nil
}

// icons converts logos and remembers what it converted.
//
// Scaling a few thousand pixels down is not free, and the tab icon is asked for
// on every first visit. Keyed by the fingerprint that is already in the icon's
// address, so a changed logo is a different key and there is nothing to
// invalidate.
type icons struct {
	mu   sync.Mutex
	key  string
	body []byte
}

// convert returns the icon for this logo, converting it at most once.
//
// A logo that cannot be converted answers nothing, and the caller serves the
// shipped mark instead: a tab with the wrong picture is a small wrong thing, and
// a tab with no picture is what this whole exercise was about.
func (c *icons) convert(logo string, decoded []byte) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := fingerprint(logo)

	if c.key == key {
		return c.body
	}

	converted, err := toIcon(decoded)
	if err != nil {
		c.key, c.body = key, nil

		return nil
	}

	c.key, c.body = key, converted

	return converted
}
