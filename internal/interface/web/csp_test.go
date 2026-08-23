package web_test

import (
	"regexp"
	"strings"
	"testing"
)

// The markup has to obey the policy the server sends with it.
//
// A violation is not an error anybody sees. The browser drops the offending
// thing and carries on, so the page still loads and only the part that needed it
// is quietly wrong - which is how a `style="display:contents"` got into the
// database form and took the layout it existed to protect with it. It worked in
// the installer, which is served by a different server that sends no policy at
// all, and that is exactly the kind of thing that makes the mistake easy.
//
// The inline-script half of the same rule lives in theme_test.go, beside the
// reason that script has to be a file.

// styleAttribute finds a style="..." on any element.
var styleAttribute = regexp.MustCompile(`<[^>]*\sstyle\s*=\s*"[^"]*"`)

func TestNoInlineStyleInTheMarkup(t *testing.T) {
	// Every page this server serves, because the policy covers all of them.
	for _, path := range []string{"/", "/api-docs"} {
		t.Run(path, func(t *testing.T) {
			for _, found := range styleAttribute.FindAllString(asset(t, path), -1) {
				t.Errorf("inline style, which style-src 'self' drops: %s", found)
			}
		})
	}
}

// And no <style> block either, which the same directive forbids for the same
// reason - the stylesheet is a file.
func TestNoStyleBlockInTheMarkup(t *testing.T) {
	for _, path := range []string{"/", "/api-docs"} {
		t.Run(path, func(t *testing.T) {
			if strings.Contains(asset(t, path), "<style") {
				t.Error("a <style> block is in the markup, which the CSP drops")
			}
		})
	}
}

// The one exception the policy does make is worth pinning, so that widening it
// further is a deliberate act rather than a copied line: images may come from a
// data: URI, because an administrator's logo is stored as one.
func TestTheOnlyDataURIsAreImages(t *testing.T) {
	page := asset(t, "/")

	for line := range strings.SplitSeq(page, "\n") {
		if !strings.Contains(line, "data:") {
			continue
		}

		if !strings.Contains(line, "data:image/") {
			t.Errorf("a data: URI that is not an image, which the CSP allows only "+
				"for img-src: %s", strings.TrimSpace(line))
		}
	}
}
