package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/dennis-dko/go-time-recording/internal/support/i18n"
	"github.com/dennis-dko/go-time-recording/internal/support/imaging"
)

// manifestPath is where a browser reads what the installed app is called and
// which icons it has.
const manifestPath = "/manifest.webmanifest"

// appIconPath is where the installed app's icons are served, followed by the size.
const appIconPath = "/app-icon/"

// appIconSizes are the squares an installed app is given: 192 for the home screen
// and 512 for the install dialog and the splash screen. Only these are drawn - an
// address taking any number would draw a picture of any size anybody asked for.
var appIconSizes = []int{192, 512}

// The colours the device paints around the app before the page has: the
// interface's own ground and accent, so starting it does not flash white into a
// dark room or blue into nothing.
const (
	appBackground = "#f5f6f8"
	appTheme      = "#2f6feb"
)

// appShortcut is one screen a long press on the installed app leads to.
//
// Named by the interface's own words: the key is the page's, the English is its
// fallback, and every other language's name is read from the dictionaries the
// page reads rather than written down here a second time. The manifest is
// fetched by the device, not by the page, so it cannot go through t() - but it
// can use what t() uses.
type appShortcut struct {
	key, english, view string
}

var appShortcuts = []appShortcut{
	{key: "nav.timesheets", english: "Time entries", view: "timesheets"},
	{key: "nav.calendar", english: "Calendar", view: "calendar"},
	{key: "nav.overtime", english: "Overtime", view: "overtime"},
}

// installable serves what makes the page installable as an app.
type installable struct {
	// words names the shortcuts, by language: read once, because the
	// dictionaries are compiled in and cannot change while the process runs.
	words map[string]i18n.Translator

	mu    sync.Mutex
	drawn map[string][]byte
}

// newInstallable reads the shortcuts' names out of the dictionaries the page
// reads, so the two cannot come to disagree.
func newInstallable() *installable {
	words := map[string]i18n.Translator{}

	for _, language := range i18n.Languages() {
		words[language] = i18n.For(language)
	}

	return &installable{words: words, drawn: map[string][]byte{}}
}

// appManifest is the Web App Manifest, the fields a device reads of it.
type appManifest struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	ShortName       string           `json:"short_name"`
	Lang            string           `json:"lang"`
	StartURL        string           `json:"start_url"`
	Scope           string           `json:"scope"`
	Display         string           `json:"display"`
	BackgroundColor string           `json:"background_color"`
	ThemeColor      string           `json:"theme_color"`
	Icons           []manifestIcon   `json:"icons"`
	Shortcuts       []manifestTarget `json:"shortcuts"`
}

type manifestIcon struct {
	Src     string `json:"src"`
	Sizes   string `json:"sizes"`
	Type    string `json:"type"`
	Purpose string `json:"purpose"`
}

type manifestTarget struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// serveManifest writes the manifest for this installation, in the language the
// device asked for.
//
// Built per request rather than shipped as a file, because two of its fields are
// settings: the name is the installation's own, and the icons carry a fingerprint
// of its logo so a changed logo is a changed address.
func (s *installable) serveManifest(w http.ResponseWriter, r *http.Request, branding Branding) {
	name := strings.TrimSpace(branding.Title)
	if name == "" {
		name = "Time Recording"
	}

	lang := preferredLanguage(r.Header.Get("Accept-Language"))
	version := appIconVersion(branding)

	manifest := appManifest{
		ID:              "/",
		Name:            name,
		ShortName:       name,
		Lang:            lang,
		StartURL:        "/",
		Scope:           "/",
		Display:         "standalone",
		BackgroundColor: appBackground,
		ThemeColor:      appTheme,
	}

	for _, size := range appIconSizes {
		side := strconv.Itoa(size)

		manifest.Icons = append(manifest.Icons, manifestIcon{
			Src:     appIconPath + side + "?v=" + version,
			Sizes:   side + "x" + side,
			Type:    "image/png",
			Purpose: "any",
		})
	}

	for _, shortcut := range appShortcuts {
		manifest.Shortcuts = append(manifest.Shortcuts, manifestTarget{
			Name: s.words[lang].Text(shortcut.key, shortcut.english),
			URL:  "/#" + shortcut.view,
		})
	}

	body, err := json.Marshal(manifest)
	if err != nil {
		http.Error(w, "the manifest could not be written", http.StatusInternalServerError)

		return
	}

	header := w.Header()
	header.Set("Content-Type", "application/manifest+json")
	// Revalidated, like the document: what it says depends on settings and on the
	// language asked for, which no address here encodes.
	header.Set("Cache-Control", "no-cache")
	header.Set("Vary", "Accept-Language")

	tag := `"` + fingerprint(string(body)) + `"`
	header.Set("ETag", tag)

	if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, tag) {
		w.WriteHeader(http.StatusNotModified)

		return
	}

	_, _ = w.Write(body)
}

// serveAppIcon writes one of the installed app's icons: the installation's logo
// where it has one, the shipped mark where it has not.
//
// The logo is fitted from the original, through the part of it chosen for the
// tab, rather than scaled up from the 64 pixels the tab is kept at.
func (s *installable) serveAppIcon(w http.ResponseWriter, r *http.Request, branding Branding) {
	size, ok := appIconSize(strings.TrimPrefix(path(r), appIconPath))
	if !ok {
		http.NotFound(w, r)

		return
	}

	key := appIconVersion(branding) + ":" + strconv.Itoa(size)

	s.mu.Lock()
	body, known := s.drawn[key]
	s.mu.Unlock()

	if !known {
		body = drawAppIcon(branding, size)

		s.mu.Lock()
		// Kept for as long as the process runs, keyed by what it was drawn from: a
		// logo changes rarely, and a stale key only ever wastes a few kilobytes.
		s.drawn[key] = body
		s.mu.Unlock()
	}

	serveImage(w, r, body, "image/png")
}

// drawAppIcon makes the picture: the logo fitted into the square, or - for an
// installation without one, or with one that cannot be read - the shipped mark.
func drawAppIcon(branding Branding, size int) []byte {
	if decoded, _, ok := decodeDataURI(branding.Logo); ok {
		if fitted, err := imaging.FitSquare(decoded, branding.IconCrop, size); err == nil {
			return fitted
		}
	}

	mark, err := imaging.MarkPNG(size)
	if err != nil {
		return nil
	}

	return mark
}

// appIconSize reads the size from the address, and only the announced ones.
func appIconSize(raw string) (int, bool) {
	size, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}

	for _, announced := range appIconSizes {
		if size == announced {
			return size, true
		}
	}

	return 0, false
}

// appIconVersion is the part of an app icon's address that changes when the
// picture would: the logo and the part of it chosen.
func appIconVersion(b Branding) string {
	return fingerprint(b.Logo + "\x00" + fmt.Sprint(b.IconCrop))
}

// preferredLanguage is the language a device asking with this Accept-Language
// would rather read, of the languages the interface speaks.
//
// The highest weight wins, and the order breaks a tie, which is what the header
// means. A device asking for none of them gets English, the language the
// interface is written in.
func preferredLanguage(header string) string {
	best, bestWeight := i18n.English, -1.0
	spoken := i18n.Languages()

	for part := range strings.SplitSeq(header, ",") {
		tag, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		language, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(tag)), "-")

		if !slices.Contains(spoken, language) {
			continue
		}

		weight := 1.0

		if q, found := strings.CutPrefix(strings.TrimSpace(params), "q="); found {
			if parsed, err := strconv.ParseFloat(q, 64); err == nil {
				weight = parsed
			}
		}

		if weight > bestWeight {
			best, bestWeight = language, weight
		}
	}

	return best
}
