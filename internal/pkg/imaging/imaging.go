// Package imaging makes the sizes of a logo an installation is shown in.
//
// A logo is uploaded once, for a header, and then drawn in three places that
// want three very different things: a mark beside the title, a banner over a
// sign-in card, and sixteen pixels in a browser tab. Handing the same few
// thousand pixels to all three is what this exists to stop - most visibly in the
// tab, where a browser given an image that size makes its own decision about
// whether to use it at all.
package imaging

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"strings"

	// Registered for image.Decode. These two and no others, which is what a logo
	// may be.
	_ "image/jpeg"
	_ "image/png"

	xdraw "golang.org/x/image/draw"
)

// The sizes a logo is kept in, one per place it is shown.
//
// Each is twice what the screen draws, because a screen at twice the density
// draws twice as many pixels for the same box - and an image at exactly the CSS
// size is soft on every laptop made in the last ten years.
//
// Derived once, when the logo is saved, rather than per request: a wordmark is a
// few thousand pixels across, and scaling it down is not free. What is stored is
// then what is served, which also means a restart has nothing to redo.
const (
	// HeaderWidth and HeaderHeight fit the mark beside the title in the top bar,
	// which CSS draws at 220 by 40.
	HeaderWidth, HeaderHeight = 440, 80

	// BannerWidth and BannerHeight fit the sign-in card, which draws it at up to
	// 328 by 96.
	BannerWidth, BannerHeight = 656, 192

	// IconSize is the square a browser tab is given.
	IconSize = 64
)

// Fit scales an image to fit inside the given box, without cropping, and returns
// it as a PNG data URI.
//
// Nothing is cut: a wordmark keeps its proportions and gains transparent bands
// where the box is a different shape. The alternative is cropping to the box,
// which for a logo means keeping whichever part happens to be in the middle.
func Fit(dataURI string, width, height int) (string, error) {
	return fitURI(dataURI, width, height, false)
}

// FitIcon is Fit for a browser tab: the same scaling, padded out to the square.
//
// Its own function rather than a flag on Fit, because the difference is not a
// detail of the scaling - it is what a tab requires and what the other two must
// not have. Deriving the icon through Fit produced a 64 by 16 strip, which a tab
// either stretches or refuses.
func FitIcon(dataURI string) (string, error) {
	return fitURI(dataURI, IconSize, IconSize, true)
}

func fitURI(dataURI string, width, height int, pad bool) (string, error) {
	decoded, _, ok := decodeDataURI(dataURI)
	if !ok {
		return "", fmt.Errorf("the logo is not an inline image")
	}

	converted, err := fitInto(decoded, width, height, pad)
	if err != nil {
		return "", err
	}

	return "data:image/png;base64," +
		base64.StdEncoding.EncodeToString(converted), nil
}

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

// ToIcon turns a stored logo into a square PNG.
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
func ToIcon(body []byte) ([]byte, error) {
	// Padded, because a tab icon has to be square: an image that is not gets one
	// dimension stretched or the whole thing refused, depending on who is asked.
	return fitInto(body, iconSize, iconSize, true)
}

// fitInto scales an image to fit inside a box.
//
// With pad, the result is exactly the box and the image sits centred in it on
// transparency - which is what a square icon needs. Without, the result is the
// image at its scaled size and nothing more, because a header and a sign-in card
// place it themselves and a transparent margin would only fight them for room.
func fitInto(body []byte, boxWidth, boxHeight int, pad bool) ([]byte, error) {
	source, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("the logo could not be read as an image: %w", err)
	}

	bounds := source.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return nil, fmt.Errorf("the logo has no size")
	}

	// The largest fit inside the box, keeping the proportions.
	//
	// Never enlarged: a small mark blown up to fill a banner is a blurred small
	// mark, and the box is a ceiling rather than a target.
	scale := min(
		float64(boxWidth)/float64(bounds.Dx()),
		float64(boxHeight)/float64(bounds.Dy()),
	)

	scale = min(scale, 1)

	width := max(1, int(float64(bounds.Dx())*scale))
	height := max(1, int(float64(bounds.Dy())*scale))

	// Transparent, so a logo that is not square keeps its shape against whatever
	// the browser draws behind it - a tab strip is light in one theme and dark in
	// the other, and a white box would be wrong in one of them.
	if !pad {
		boxWidth, boxHeight = width, height
	}

	icon := image.NewRGBA(image.Rect(0, 0, boxWidth, boxHeight))

	at := image.Rect(
		(boxWidth-width)/2, (boxHeight-height)/2,
		(boxWidth-width)/2+width, (boxHeight-height)/2+height,
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

// decodeDataURI unpacks an inline image.
//
// Only base64 PNG and JPEG, which is what a logo may be. Anything else is not
// decoded rather than passed through: what comes out of here is served as an
// image, and a type somebody else chose is how that stops being one.
func decodeDataURI(uri string) (body []byte, contentType string, ok bool) {
	head, encoded, found := strings.Cut(uri, ",")
	if !found || !strings.HasSuffix(head, ";base64") {
		return nil, "", false
	}

	kind := strings.TrimSuffix(strings.TrimPrefix(head, "data:"), ";base64")

	switch kind {
	case "image/png", "image/jpeg":
	default:
		return nil, "", false
	}

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(decoded) == 0 {
		return nil, "", false
	}

	return decoded, kind, true
}
