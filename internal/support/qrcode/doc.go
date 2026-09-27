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
