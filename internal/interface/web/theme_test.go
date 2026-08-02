package web_test

import (
	"strings"
	"testing"
)

// The theme has to be chosen before the browser paints, or the page shows in
// the wrong colours for a moment and then swaps - the "flash of wrong theme".
//
// Two things make that work, and both are easy to undo by accident: the script
// runs in <head> without defer or async, and it is a file rather than an inline
// block, because the Content-Security-Policy forbids inline script. These tests
// hold both in place.

func TestThemeScriptRunsBeforeTheBodyIsPainted(t *testing.T) {
	body := asset(t, "/")

	head := body[:strings.Index(body, "</head>")]
	if !strings.Contains(head, `src="/theme.js"`) {
		t.Fatal("theme.js must be loaded in <head>, or the page flashes the wrong theme")
	}

	// defer or async would let the body render first, which is the whole
	// problem this file exists to avoid.
	tagStart := strings.Index(head, `<script src="/theme.js"`)

	tag := head[tagStart : tagStart+strings.Index(head[tagStart:], ">")+1]
	if strings.Contains(tag, "defer") || strings.Contains(tag, "async") {
		t.Errorf("theme.js must load synchronously, got %q", tag)
	}
}

// An inline script would be blocked by the Content-Security-Policy, which
// permits only same-origin files.
func TestNoInlineScriptInTheMarkup(t *testing.T) {
	body := asset(t, "/")

	for _, tag := range strings.Split(body, "<script")[1:] {
		open := tag[:strings.Index(tag, ">")+1]
		if !strings.Contains(open, "src=") {
			t.Errorf("inline script found, which the CSP blocks: <script%s", open)
		}
	}
}

// theme.js is loaded before app.js can define anything, so it must stand alone.
func TestThemeScriptIsSelfContained(t *testing.T) {
	script := asset(t, "/theme.js")

	if !strings.Contains(script, "data-theme") && !strings.Contains(script, "dataset.theme") {
		t.Error("theme.js should stamp the theme onto the document element")
	}

	// It runs before app.js, so leaning on anything from there would throw.
	for _, forbidden := range []string{"TRANSLATIONS", "api(", "$('"} {
		if strings.Contains(script, forbidden) {
			t.Errorf("theme.js must not depend on app.js, but references %q", forbidden)
		}
	}
}

// Both explicit choices must override the operating system's preference, in
// both directions: picking light on a machine set to dark has to give light.
func TestExplicitThemesOverrideTheSystemPreference(t *testing.T) {
	css := asset(t, "/app.css")

	for _, selector := range []string{`:root[data-theme="dark"]`, `:root[data-theme="light"]`} {
		if !strings.Contains(css, selector) {
			t.Errorf("expected %s in the stylesheet", selector)
		}
	}

	// The media query must not apply once a theme has been stamped, or it would
	// fight the explicit choice.
	if !strings.Contains(css, `:root:not([data-theme])`) {
		t.Error("the prefers-color-scheme block must yield to an explicit theme")
	}

	// Ordering decides which wins; the explicit rules have to come last.
	media := strings.Index(css, "@media (prefers-color-scheme: dark)")
	explicit := strings.Index(css, `:root[data-theme="dark"]`)

	if media < 0 || explicit < 0 || explicit < media {
		t.Error("the explicit theme rules must come after the media query")
	}
}

// The stylesheet must tell the browser which palette form controls and
// scrollbars sit on, or they stay light inside a dark page.
func TestColorSchemeIsDeclared(t *testing.T) {
	css := asset(t, "/app.css")

	if strings.Count(css, "color-scheme:") < 2 {
		t.Error("expected color-scheme to be declared for both themes")
	}
}

// The rule that keeps hidden elements hidden has bitten before: without it,
// display:flex on .login-screen beats the browser's own [hidden] rule and the
// sign-in overlay never goes away.
func TestHiddenAttributeStillWins(t *testing.T) {
	css := asset(t, "/app.css")

	if !strings.Contains(css, "[hidden] { display: none !important; }") {
		t.Error("the global [hidden] rule must stay, or hidden elements render anyway")
	}
}
