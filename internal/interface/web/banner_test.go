package web_test

import (
	"regexp"
	"strings"
	"testing"
)

// A banner cannot be dismissed, and nothing may look as though it can be.
//
// CLAUDE.md states the rule and index.html states the incident behind it: the
// release banner was dismissable once, the dismissal was remembered against that
// version, and the result was an installation three releases behind because
// somebody clicked a cross on a busy morning. A state that has been clicked away
// is still the state.
//
// The rule was enforced by everybody remembering it. What was left behind was a
// .banner-dismiss rule in the stylesheet - two rules, styled down to the hover
// opacity - for a control that no longer exists in any markup or script. Nothing
// reports that: there is no build step, so nothing tree-shakes the stylesheet,
// and a Go dead-code scan cannot see a CSS class at all.
//
// A leftover rule is not weight. A class called .banner-dismiss is an invitation
// to add the button back under a name that already has styling waiting for it,
// which is how a removed decision returns as a small tidy commit.
func TestNoBannerOffersAWayToDismissIt(t *testing.T) {
	css := asset(t, "/app.css")
	html := asset(t, "/")
	js := asset(t, "/app.js")

	// The stylesheet first: a selector is the part that outlives the control.
	dismissal := regexp.MustCompile(`\.banner[\w-]*dismiss[\w-]*`)

	for _, found := range dismissal.FindAllString(stripCSSComments(css), -1) {
		t.Errorf("app.css styles %s, and a banner has nothing to dismiss it with; "+
			"if the control is gone the rule goes with it, and if it is back the "+
			"rule in CLAUDE.md is what has to change first", found)
	}

	// And the markup, which is where it would come back.
	//
	// Comments are stripped first, and that is not a detail: the reasoning for
	// why these are not dismissable is written beside them, at length, and it uses
	// the word. Matching the prose that explains the rule as a breach of the rule
	// is the failure mode that makes a check like this get deleted.
	for _, banner := range bannersIn(stripHTMLComments(html)) {
		if strings.Contains(banner, "dismiss") || strings.Contains(banner, "&times;") {
			t.Errorf("index.html builds a banner that offers a dismissal: %s",
				summarise(banner))
		}
	}

	// app.js fills #instance-banner and #maintenance-banner, so a dismissal added
	// there would never appear in the markup at all.
	dismissalNearABanner := regexp.MustCompile(
		`(?i)banner[^\n]*\b(dismiss|&times;)`)
	if dismissalNearABanner.MatchString(stripLineComments(js)) {
		t.Error("app.js builds a banner that offers a dismissal")
	}
}

// bannersIn returns each banner element, from its opening tag to the first
// closing tag that is not one of its own children's.
func bannersIn(source string) []string {
	var out []string

	opening := regexp.MustCompile(`<div[^>]*(?:id|class)="[^"]*banner[^"]*"[^>]*>`)

	for _, at := range opening.FindAllStringIndex(source, -1) {
		out = append(out, source[at[0]:closingOf(source, at[1])])
	}

	return out
}

// closingOf walks forward counting div nesting, so a banner carrying a row of
// controls is read to its own end rather than to its first child's.
func closingOf(source string, from int) int {
	depth := 1

	for i := from; i < len(source); i++ {
		switch {
		case strings.HasPrefix(source[i:], "<div"):
			depth++
		case strings.HasPrefix(source[i:], "</div>"):
			depth--

			if depth == 0 {
				return i
			}
		}
	}

	return len(source)
}

func stripCSSComments(css string) string {
	return regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(css, " ")
}

func stripHTMLComments(html string) string {
	return regexp.MustCompile(`(?s)<!--.*?-->`).ReplaceAllString(html, " ")
}

func stripLineComments(js string) string {
	js = regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(js, " ")

	return regexp.MustCompile(`(?m)^\s*//.*$`).ReplaceAllString(js, " ")
}

func summarise(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 160 {
		return s[:160] + "…"
	}

	return s
}
