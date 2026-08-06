// Package qrcode renders a string as a scannable QR code, as an SVG data URI.
//
// SVG rather than a bitmap because a QR code is a grid of squares: it scales to
// any screen without the softening that stops a phone reading it, and one path of
// module outlines is a fraction of the bytes a PNG of the same code would be.
//
// A data URI rather than an endpoint of its own so the code arrives with the
// secret it encodes, in the same response. The interface's Content-Security-Policy
// already allows data: images - the favicon and the instance logo are both stored
// that way - so nothing had to be loosened for this.
package qrcode

import (
	"encoding/base64"
	"errors"
	"strconv"
	"strings"

	"rsc.io/qr"
)

// quietZone is the four-module margin the specification requires around a code.
//
// Without it a scanner has nothing to separate the finder patterns from whatever
// is next to them on screen, and reading becomes unreliable in exactly the way
// that is hard to reproduce: it works on the developer's phone against a white
// card and fails against a dark background.
const quietZone = 4

// ErrEmpty is returned for an empty string, which has no code to draw.
var ErrEmpty = errors.New("nothing to encode")

// SVGDataURI renders text as an SVG QR code, ready for the src of an img.
//
// Error correction level M: it recovers about 15% of the code, which is what
// survives a fingerprint or a glare spot on a screen, without the size increase of
// the higher levels. An otpauth URI is short enough that the difference is a few
// modules either way.
func SVGDataURI(text string) (string, error) {
	svg, err := SVG(text)
	if err != nil {
		return "", err
	}

	// Base64 rather than percent-encoding: the markup is full of characters that
	// would each need escaping, and the result would be longer than the encoding.
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(svg)), nil
}

// SVG renders text as an SVG document, in module coordinates.
//
// No width or height attributes: the viewBox alone lets the interface size it with
// CSS, so the same code serves a printed sheet and a phone held up to a laptop.
func SVG(text string) (string, error) {
	if text == "" {
		return "", ErrEmpty
	}

	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return "", err
	}

	side := code.Size + 2*quietZone

	var out strings.Builder

	out.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 `)
	out.WriteString(strconv.Itoa(side))
	out.WriteString(" ")
	out.WriteString(strconv.Itoa(side))
	out.WriteString(`" shape-rendering="crispEdges">`)

	// The quiet zone has to be drawn rather than left transparent: on a dark theme
	// a transparent margin is dark, and a code needs light around it.
	out.WriteString(`<rect width="100%" height="100%" fill="#ffffff"/>`)

	out.WriteString(`<path fill="#000000" d="`)

	for y := 0; y < code.Size; y++ {
		for x := 0; x < code.Size; x++ {
			if !code.Black(x, y) {
				continue
			}

			// One module as a closed unit square. Cheaper than a rect element
			// each, and the whole grid ends up in a single path.
			out.WriteString("M")
			out.WriteString(strconv.Itoa(x + quietZone))
			out.WriteString(" ")
			out.WriteString(strconv.Itoa(y + quietZone))
			out.WriteString("h1v1h-1z")
		}
	}

	out.WriteString(`"/></svg>`)

	return out.String(), nil
}
