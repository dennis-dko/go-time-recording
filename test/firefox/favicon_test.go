//go:build firefox

package firefox

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/test/harness"
)

// The configured logo is the only icon the page declares.
//
// Reported against Firefox and true there: the logo reached the tab in Chrome
// and did not in Firefox, from the same markup and the same script.
//
// The page ships two icons - an SVG, and an .ico beside it as
// `rel="alternate icon"` for anything that cannot read the first. The script
// looked for `link[rel="icon"]`, which matches the former and not the latter,
// because an attribute selector compares the whole value. So it added the logo,
// removed the SVG, and left the .ico standing. The page then declared two icons
// and each engine chose by its own rules: Chrome took the logo, Firefox kept the
// .ico. Nothing was wrong with the logo.
//
// Which is why this is asserted as a count and not as "the logo is present
// somewhere". Present somewhere was true throughout.
func TestTheConfiguredLogoIsTheOnlyIconDeclared(t *testing.T) {
	app := harness.Start(t)
	b := openBrowser(t)

	b.goTo(app.BaseURL())
	signIn(t, b)

	const logo = "data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHdpZHRoPSI2MDAiIGhlaWdodD0iMTIwIj48cmVjdCB3aWR0aD0iNjAwIiBoZWlnaHQ9IjEyMCIgZmlsbD0iIzFmNGU3OSIvPjwvc3ZnPg=="

	post(t, b, "/api/v1/settings/branding",
		fmt.Sprintf(`{"title":"Zeiterfassung","logo":%q}`, logo), "PUT")

	b.goTo(app.BaseURL())
	b.settle()

	icons := declaredIcons(t, b)

	if len(icons) != 1 {
		t.Fatalf("the page declares %d icons (%v); with more than one, which the "+
			"tab shows is the engine's choice rather than this application's",
			len(icons), icons)
	}

	if !strings.HasPrefix(icons[0], "data:image/") {
		t.Errorf("the declared icon is %.40q rather than the configured logo", icons[0])
	}

	// And clearing it puts the shipped pair back rather than leaving the page
	// with no icon at all.
	post(t, b, "/api/v1/settings/branding", `{"title":"Zeiterfassung","logo":""}`, "PUT")

	b.goTo(app.BaseURL())
	b.settle()

	restored := declaredIcons(t, b)

	if len(restored) == 0 {
		t.Fatal("clearing the logo left the page with no icon declared at all")
	}

	for _, href := range restored {
		if strings.HasPrefix(href, "data:image/") {
			t.Errorf("the cleared logo is still declared as an icon: %.40q", href)
		}
	}
}

// declaredIcons is every icon link on the page, in document order.
func declaredIcons(t *testing.T, b *browser) []string {
	t.Helper()

	var icons []string

	b.evalJSON(`JSON.stringify([...document.querySelectorAll('link[rel~="icon"]')]
		.map(link => link.getAttribute('href')))`, &icons)

	return icons
}
