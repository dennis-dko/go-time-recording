package web_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/interface/web"
)

// The page arrives already carrying the instance's own name.
//
// It used to arrive with "Time Recording" and a script that corrected it once
// /branding had answered, so every reload showed a name nobody chose for as long
// as that request took. Reported twice, and both times the fix was on the wrong
// side of the round trip: there is no arrangement of client code that wins a race
// against a request it has to make first.
func TestTheDocumentCarriesTheConfiguredTitle(t *testing.T) {
	server := brandedServer(web.Branding{Title: "Zeiterfassung htp"})

	res := fetch(t, server, "/")

	if !strings.Contains(res.Body.String(), "<title>Zeiterfassung htp</title>") {
		t.Errorf("the page does not name the instance: %.200s", head(res.Body.String()))
	}

	if strings.Contains(res.Body.String(), "<title>Time Recording</title>") {
		t.Error("the shipped title is still in the document somebody is sent")
	}
}

// A title is text, and it lands between two tags.
//
// The one place in this application where configured text becomes part of the
// document rather than the text of a node - everything else goes through
// textContent in the browser, where this cannot arise. So it is the one place
// that has to escape, and it is written by whoever holds settings:manage.
func TestAConfiguredTitleCannotCloseItsOwnTag(t *testing.T) {
	server := brandedServer(web.Branding{
		Title: `</title><script>window.owned=1</script>`,
	})

	body := fetch(t, server, "/").Body.String()

	if strings.Contains(body, "<script>window.owned") {
		t.Error("a configured title escaped its tag and became markup")
	}

	if !strings.Contains(body, "&lt;/title&gt;") {
		t.Errorf("the title was not escaped: %.200s", head(body))
	}
}

// Nothing is left with no title at all.
func TestAnEmptyTitleFallsBackToTheShippedOne(t *testing.T) {
	server := brandedServer(web.Branding{Title: "   "})

	if !strings.Contains(fetch(t, server, "/").Body.String(), "<title>Time Recording</title>") {
		t.Error("an instance that has named itself nothing has no name at all")
	}
}

// The page declares exactly one icon, and it is a URL.
//
// Two icons was the Firefox bug: the markup ships an SVG and an .ico beside it,
// the script replaced one and left the other, and each engine then chose by its
// own rules. A data: URI was the second attempt and did not work either -
// engines disagree about honouring one, and about honouring an icon link that
// changed after the document was parsed.
//
// A URL in the served document is the thing they all do the same.
func TestTheDocumentDeclaresOneIconAndItIsAnAddress(t *testing.T) {
	server := brandedServer(web.Branding{Title: "x", Logo: pngLogo()})

	body := fetch(t, server, "/").Body.String()

	if got := strings.Count(body, `rel="icon"`); got != 1 {
		t.Errorf("the page declares %d icons; with more than one, which the tab "+
			"shows is the engine's choice rather than this application's", got)
	}

	if strings.Contains(body, "alternate icon") {
		t.Error("the second icon link is still in the document")
	}

	if strings.Contains(body, "data:image/") {
		t.Error("the icon is a data: URI, which is what did not work")
	}

	if !strings.Contains(body, `href="/favicon?v=`) {
		t.Errorf("the icon has no fingerprinted address: %.300s", head(body))
	}
}

// The address changes when the logo does, so nothing shows a cached one.
func TestADifferentLogoIsADifferentAddress(t *testing.T) {
	first := fetch(t, brandedServer(web.Branding{Logo: pngLogo()}), "/").Body.String()
	second := fetch(t, brandedServer(web.Branding{Logo: otherLogo()}), "/").Body.String()

	if iconHref(first) == iconHref(second) {
		t.Errorf("two different logos are served from one address (%s), so a "+
			"browser has no reason to fetch the second", iconHref(first))
	}
}

// The icon endpoint answers with the logo itself.
func TestTheIconEndpointServesTheConfiguredLogo(t *testing.T) {
	server := brandedServer(web.Branding{Logo: pngLogo()})

	res := fetch(t, server, "/favicon")

	if res.Code != http.StatusOK {
		t.Fatalf("the icon answered %d", res.Code)
	}

	if got := res.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("the icon is served as %q, so a browser may not draw it", got)
	}

	if res.Body.Len() == 0 {
		t.Error("the icon is empty")
	}
}

// With nothing configured, the shipped mark is served rather than a 404.
func TestTheIconEndpointFallsBackToTheShippedMark(t *testing.T) {
	res := fetch(t, brandedServer(web.Branding{}), "/favicon")

	if res.Code != http.StatusOK {
		t.Fatalf("an instance with no logo has no tab icon at all (%d)", res.Code)
	}

	if !strings.Contains(res.Header().Get("Content-Type"), "svg") {
		t.Errorf("the shipped mark is served as %q", res.Header().Get("Content-Type"))
	}
}

// A logo that is not an image is not served as one.
//
// The logo is stored as a data URI and its type comes from that string, which is
// written by whoever holds settings:manage. Passing the type through would let
// this endpoint answer with a caller-chosen content type - which is how an image
// endpoint stops being one.
func TestALogoThatIsNotAnImageIsRefused(t *testing.T) {
	for name, logo := range map[string]string{
		"a page": "data:text/html;base64," +
			base64.StdEncoding.EncodeToString([]byte("<script>1</script>")),
		"an image type nobody has": "data:image/pretend;base64," +
			base64.StdEncoding.EncodeToString([]byte("nonsense")),
		"not encoded at all": "data:image/png,plain",
	} {
		t.Run(name, func(t *testing.T) {
			res := fetch(t, brandedServer(web.Branding{Logo: logo}), "/favicon")

			if got := res.Header().Get("Content-Type"); !strings.Contains(got, "svg") {
				t.Errorf("the icon endpoint answered with %q for %s", got, name)
			}
		})
	}
}

// fetch performs one request against a handler.
func fetch(t *testing.T, server http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	return res
}

// iconHref is the address the document points its icon at.
func iconHref(body string) string {
	const marker = `href="/favicon`

	at := strings.Index(body, marker)
	if at < 0 {
		return ""
	}

	rest := body[at+len(`href="`):]

	return rest[:strings.Index(rest, `"`)]
}

func head(body string) string {
	if at := strings.Index(body, "<body"); at > 0 {
		return body[:at]
	}

	return body
}

// A one-pixel PNG and a one-pixel GIF: two logos that differ, which is all these
// cases need of them.
func pngLogo() string {
	return "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
}

func otherLogo() string {
	return "data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7"
}

// A wordmark is converted into something a tab can use.
//
// This is what was missing all along: the logo was served exactly as stored, so
// what reached the browser as a tab icon was a few thousand pixels of wordmark,
// twice as wide as it is tall. Every decision was left to the browser - whether
// to accept it at that size, how to fit two-to-one into a square, whether to
// bother at all - and the answers differ by engine. One that decides not to shows
// nothing and says nothing, which is a long way from any error message.
func TestAWordmarkIsConvertedIntoASquareIcon(t *testing.T) {
	logo := rasterLogo(t, 2776, 1299)

	server := brandedServer(web.Branding{Title: "x", Logo: logo})

	res := fetch(t, server, "/favicon")

	if res.Code != http.StatusOK {
		t.Fatalf("the icon answered %d", res.Code)
	}

	if got := res.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("the icon is served as %q; a converted one is always a PNG", got)
	}

	icon, format, err := image.Decode(bytes.NewReader(res.Body.Bytes()))
	if err != nil {
		t.Fatalf("what was served is not an image: %v", err)
	}

	if format != "png" {
		t.Errorf("the icon is a %s", format)
	}

	bounds := icon.Bounds()

	// Square, and small. Those are the two properties that make an icon
	// uninteresting to every engine that receives it.
	if bounds.Dx() != bounds.Dy() {
		t.Errorf("the icon is %dx%d, which is not square", bounds.Dx(), bounds.Dy())
	}

	if bounds.Dx() > 128 {
		t.Errorf("the icon is %dpx across; a tab draws sixteen", bounds.Dx())
	}

	// Nothing is cut: a wordmark fitted into a square leaves transparent bands
	// above and below, and the corners are where they are.
	if _, _, _, alpha := icon.At(1, 1).RGBA(); alpha != 0 {
		t.Error("the icon was cropped to the square rather than fitted into it; " +
			"part of the logo has been cut off")
	}

	// And something was actually drawn, rather than a transparent square being
	// served as a successful conversion.
	if !hasVisiblePixels(icon) {
		t.Error("the icon is empty")
	}
}

// A logo that cannot be converted leaves the shipped mark in place.
//
// A tab with the wrong picture is a small wrong thing. A tab with no picture is
// what this whole exercise was about, so it must not be the answer to anything.
func TestALogoThatCannotBeConvertedFallsBackToTheShippedMark(t *testing.T) {
	// The right declaration and rubbish behind it, which is what a truncated
	// upload looks like from here.
	broken := "data:image/png;base64," +
		base64.StdEncoding.EncodeToString([]byte("not really a PNG"))

	res := fetch(t, brandedServer(web.Branding{Logo: broken}), "/favicon")

	if res.Code != http.StatusOK {
		t.Fatalf("a logo that cannot be read left the tab with no icon at all (%d)",
			res.Code)
	}

	if !strings.Contains(res.Header().Get("Content-Type"), "svg") {
		t.Errorf("the fallback is served as %q rather than the shipped mark",
			res.Header().Get("Content-Type"))
	}
}

// rasterLogo is a PNG of the given size, shaped like a wordmark: a mark on the
// left, a band of text to the right of it, transparent everywhere else.
func rasterLogo(t *testing.T, width, height int) string {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))

	for y := range height {
		for x := range width {
			dx, dy := x-width/5, y-height/2

			switch {
			case dx*dx+dy*dy < (height/3)*(height/3):
				img.Set(x, y, color.RGBA{R: 205, G: 220, A: 255})
			case x > width/2 && y > height/3 && y < 2*height/3:
				img.Set(x, y, color.RGBA{A: 255})
			}
		}
	}

	var out bytes.Buffer

	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}

	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(out.Bytes())
}

// hasVisiblePixels reports whether anything was drawn at all.
func hasVisiblePixels(img image.Image) bool {
	bounds := img.Bounds()

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if _, _, _, alpha := img.At(x, y).RGBA(); alpha > 0 {
				return true
			}
		}
	}

	return false
}
