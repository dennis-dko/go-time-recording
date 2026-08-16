package web

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io/fs"
	"net/http"
	"strings"
)

// iconPath is where the browser tab's icon comes from.
//
// One address whatever is configured, so the markup never changes and every
// engine simply fetches it. What comes back is the instance's logo where there is
// one and the shipped mark where there is not.
const iconPath = "/favicon"

// isDocument reports whether this request is for the single page itself rather
// than for one of the files it pulls in.
//
// The single-page fallback has already rewritten anything unknown to "/", so by
// the time this is asked the only paths that mean the document are these two.
func isDocument(p string) bool {
	return p == "/" || p == "/index.html"
}

// document is index.html taken apart once, so a request only has to put it back
// together with this installation's own name and mark in it.
type document struct {
	// The three pieces around the two things that are substituted. Empty when the
	// markup did not contain what was expected, in which case the file is served
	// as it is - a missing title is a worse failure than a generic one.
	before, middle, after string
	usable                bool
}

// The exact strings replaced. Kept as constants beside the markup they match,
// because a change to index.html that misses this file should fail loudly here
// rather than quietly serve a document with nothing substituted.
const (
	titleMarkup = "<title>Time Recording</title>"
	iconMarkup  = `<link rel="icon" type="image/svg+xml" href="/favicon.svg">
  <link rel="alternate icon" href="/favicon.ico">`
)

// newDocument splits index.html around the title and the icon links.
func newDocument(sub fs.FS) document {
	body, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		return document{}
	}

	markup := string(body)

	titleAt := strings.Index(markup, titleMarkup)
	iconAt := strings.Index(markup, iconMarkup)

	// The title comes first in the markup and the icons after it. If either is
	// missing, or they have been reordered, this stops substituting rather than
	// guessing - the assets are compiled in, so this is decided once at start-up
	// and is the same for every request.
	if titleAt < 0 || iconAt < titleAt+len(titleMarkup) {
		return document{}
	}

	return document{
		before: markup[:titleAt],
		middle: markup[titleAt+len(titleMarkup) : iconAt],
		after:  markup[iconAt+len(iconMarkup):],
		usable: true,
	}
}

// serve writes the page with this installation's name and mark already in it.
func (d document) serve(w http.ResponseWriter, r *http.Request, branding Branding) {
	if !d.usable {
		http.Error(w, "the interface could not be assembled", http.StatusInternalServerError)

		return
	}

	title := strings.TrimSpace(branding.Title)
	if title == "" {
		title = "Time Recording"
	}

	// The icon's address carries a fingerprint of what it will answer with, so a
	// changed logo is a changed URL. Without it a browser that cached the old one
	// keeps showing it for as long as it feels like, which is the whole difficulty
	// with tab icons.
	icon := `<link rel="icon" href="` + iconPath + `?v=` + fingerprint(branding.Logo) + `">`

	page := d.before + "<title>" + escape(title) + "</title>" + d.middle + icon + d.after

	header := w.Header()
	header.Set("Content-Type", "text/html; charset=utf-8")

	// Revalidated rather than cached: the document now depends on a setting, so
	// yesterday's copy can be wrong in a way the file's own hash cannot express.
	header.Set("Cache-Control", "no-cache")

	tag := `"` + fingerprint(page) + `"`
	header.Set("ETag", tag)

	if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, tag) {
		w.WriteHeader(http.StatusNotModified)

		return
	}

	_, _ = w.Write([]byte(page))
}

// escape makes a string safe to put between two tags.
//
// The title is written by an administrator and rendered into markup, which is the
// one place in this application where configured text becomes part of the
// document rather than the text of a node. Everything else goes through
// textContent in the browser, where this cannot arise.
func escape(s string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
	).Replace(s)
}

// fingerprint is a short stable digest, used for cache validators rather than
// for anything that needs to resist an adversary.
func fingerprint(s string) string {
	sum := sha256.Sum256([]byte(s))

	return hex.EncodeToString(sum[:8])
}

// shippedIcon is the mark an instance carries when nothing has been configured.
func shippedIcon(sub fs.FS) []byte {
	body, err := fs.ReadFile(sub, "favicon.svg")
	if err != nil {
		return nil
	}

	return body
}

// serveIcon answers the tab icon: the configured logo, converted, or the shipped
// mark.
//
// Converted rather than passed through. What an installation uploads is a
// wordmark made for a header - a few thousand pixels across, twice as wide as it
// is tall - and handing that to a browser as a tab icon leaves every decision to
// the browser, including whether to use it at all. See toIcon.
func serveIcon(
	w http.ResponseWriter, r *http.Request,
	branding BrandingFunc, shipped []byte, cache *icons,
) {
	body, contentType := shipped, "image/svg+xml"

	if branding != nil {
		current := branding(r.Context())

		// Already the right size and shape, made when it was saved. Nothing to
		// decide, nothing to scale, and the same bytes after a restart.
		if decoded, kind, ok := decodeDataURI(current.Icon); ok {
			serveImage(w, r, decoded, kind)

			return
		}

		// An installation whose logo was saved before the sizes were derived. The
		// original is a wordmark, so it is converted rather than served.
		logo := current.Logo

		if decoded, _, ok := decodeDataURI(logo); ok {
			// A logo that cannot be converted leaves the shipped mark in place: a
			// tab with the wrong picture is a small wrong thing, and a tab with no
			// picture is the thing being fixed.
			if converted := cache.convert(logo, decoded); converted != nil {
				body, contentType = converted, "image/png"
			}
		}
	}

	serveImage(w, r, body, contentType)
}

// serveImage writes one image, cached hard against a fingerprint of itself.
func serveImage(w http.ResponseWriter, r *http.Request, body []byte, contentType string) {
	if len(body) == 0 {
		http.NotFound(w, r)

		return
	}

	header := w.Header()
	header.Set("Content-Type", contentType)

	// A year, because the address carries a fingerprint of the contents: a
	// different logo is a different URL, so this copy can never be the wrong one.
	header.Set("Cache-Control", "public, max-age=31536000, immutable")

	tag := `"` + fingerprint(string(body)) + `"`
	header.Set("ETag", tag)

	if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, tag) {
		w.WriteHeader(http.StatusNotModified)

		return
	}

	_, _ = w.Write(body)
}

// decodeDataURI unpacks the logo, which is stored inline so it travels with the
// database rather than with the filesystem.
//
// Only base64 images, which is what the interface stores and what the settings
// endpoint accepts. Anything else is not decoded rather than passed through: this
// answer becomes an image on a page, and a caller-chosen content type is how that
// stops being an image.
func decodeDataURI(uri string) (body []byte, contentType string, ok bool) {
	const prefix = "data:image/"

	if !strings.HasPrefix(uri, prefix) {
		return nil, "", false
	}

	head, encoded, found := strings.Cut(uri, ",")
	if !found || !strings.HasSuffix(head, ";base64") {
		return nil, "", false
	}

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(decoded) == 0 {
		return nil, "", false
	}

	// "data:image/png;base64" -> "image/png". Bounded to the types an image can
	// be, so nothing here can name text/html.
	kind := strings.TrimSuffix(strings.TrimPrefix(head, "data:"), ";base64")

	switch kind {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/svg+xml", "image/x-icon":
		return decoded, kind, true
	default:
		return nil, "", false
	}
}
