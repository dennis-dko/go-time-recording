//go:build firefox

package firefox

import (
	"fmt"
	"strconv"
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

	// And what that one address answers with is the logo, which is the question
	// the tab actually asks. A href that looks right while the shipped mark comes
	// back is exactly the shape this bug had twice.
	if width := iconWidth(t, b); width != 600 {
		t.Errorf("the tab icon is served at %dpx wide; the configured logo is 600", width)
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

	if width := iconWidth(t, b); width == 600 {
		t.Error("after clearing the logo the tab is still served it")
	}

	if width := iconWidth(t, b); width == 0 {
		t.Error("with no logo the tab has no icon at all")
	}
}

// iconWidth fetches whatever the page points its icon at and measures it.
//
// The bytes rather than the address: what a browser draws in a tab is the
// picture, and both earlier attempts at this bug were correct in the DOM.
func iconWidth(t *testing.T, b *browser) int {
	t.Helper()

	raw := b.evalString(`(async () => {
		const link = document.querySelector('link[rel~="icon"]');
		if (!link) return '0';

		return await new Promise((resolve) => {
			const probe = new Image();
			probe.onload = () => resolve(String(probe.naturalWidth));
			probe.onerror = () => resolve('0');
			probe.src = link.href;
		});
	})()`)

	width, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0
	}

	return width
}

// declaredIcons is every icon link on the page, in document order.
func declaredIcons(t *testing.T, b *browser) []string {
	t.Helper()

	var icons []string

	b.evalJSON(`JSON.stringify([...document.querySelectorAll('link[rel~="icon"]')]
		.map(link => link.getAttribute('href')))`, &icons)

	return icons
}

// A wide, mostly-black wordmark reaches the tab in Firefox.
//
// Reported as "no favicon at all", with a logo of that shape - so this is the
// shape the check uses rather than the tidy rectangle the case above has. What it
// answers is a narrow question: does Firefox fetch and decode what this
// application serves for that image. Whether sixteen pixels of black on a dark
// tab bar can be *seen* is a different question, and not one any test can ask.
func TestAWideDarkWordmarkStillReachesTheTab(t *testing.T) {
	app := harness.Start(t)
	b := openBrowser(t)

	b.goTo(app.BaseURL())
	signIn(t, b)

	post(t, b, "/api/v1/settings/branding",
		`{"title":"Zeiterfassung","logo":"data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHdpZHRoPSIyMDAwIiBoZWlnaHQ9IjEyOTkiIHZpZXdCb3g9IjAgMCAyMDAwIDEyOTkiPjxjaXJjbGUgY3g9IjQ3MCIgY3k9IjY1MCIgcj0iMzMwIiBmaWxsPSIjMDAwIi8+PGNpcmNsZSBjeD0iNDcwIiBjeT0iNjUwIiByPSIyMzAiIGZpbGw9IiNjZGRjMDAiLz48Y2lyY2xlIGN4PSI0NzAiIGN5PSI2NTAiIHI9IjgwIiBmaWxsPSIjMDAwIi8+PHJlY3QgeD0iOTAwIiB5PSI0NTAiIHdpZHRoPSIxMDAwIiBoZWlnaHQ9IjQwMCIgZmlsbD0iIzAwMCIvPjwvc3ZnPg=="}`, "PUT")

	b.goTo(app.BaseURL())
	b.settle()

	if icons := declaredIcons(t, b); len(icons) != 1 {
		t.Fatalf("the page declares %d icons", len(icons))
	}

	// 2000 is the wordmark's own width: Firefox fetched it, decoded it, and knows
	// how big it is. Anything else means it never became an image.
	if width := iconWidth(t, b); width != 2000 {
		t.Errorf("Firefox reads the tab icon as %dpx wide; the logo is 2000, so it "+
			"was not fetched or not decoded", width)
	}
}
