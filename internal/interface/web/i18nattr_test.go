package web_test

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Every translated attribute can find its way back to English.
//
// applyLanguage translates four kinds of thing, and each needs the same two
// halves: a copy of the English source taken the first time it is translated,
// and that copy put back when the dictionary has nothing for the key.
//
// The second half is not optional here, because TRANSLATIONS.en is deliberately
// empty - "nothing for the key" is exactly what English is. A branch that only
// assigns therefore works in one direction and never comes back: aria-label was
// written that way, and going to German and back left all seven of them in
// German on an otherwise English page, told only to the readers who cannot see
// the label printed beside the control.
//
// So this reads the kinds the markup actually uses and requires each to keep a
// source, rather than naming the four that exist today.
func TestEveryTranslatedAttributeKeepsItsEnglishSource(t *testing.T) {
	html := asset(t, "/")
	js := asset(t, "/app.js")

	kinds := map[string]bool{}
	for _, match := range regexp.MustCompile(`data-i18n(-[a-z]+)?=`).
		FindAllStringSubmatch(html, -1) {
		kinds[match[1]] = true
	}

	if len(kinds) < 2 {
		t.Fatalf("found %d kinds of translated attribute in the markup, which is "+
			"too few to be reading it correctly", len(kinds))
	}

	// applyLanguage alone: the dataset keys appear nowhere else, and a match from
	// some other function would not mean the language switch restores anything.
	body := applyLanguageBody(t, js)

	var missing []string

	for suffix := range kinds {
		// data-i18n -> i18nSource, data-i18n-aria -> i18nAriaSource.
		name := "i18n"
		if suffix != "" {
			part := strings.TrimPrefix(suffix, "-")
			name += strings.ToUpper(part[:1]) + part[1:]
		}

		name += "Source"

		if strings.Contains(body, name) {
			continue
		}

		missing = append(missing, "data-i18n"+suffix+" (expected "+name+")")
	}

	sort.Strings(missing)

	for _, kind := range missing {
		t.Errorf("applyLanguage translates %s without keeping the English source, "+
			"so a page switched to German and back keeps the German one - "+
			"TRANSLATIONS.en is empty, which is the case that needs the source", kind)
	}
}

// applyLanguageBody returns the source of applyLanguage, up to the next
// declaration at the top level.
func applyLanguageBody(t *testing.T, js string) string {
	t.Helper()

	const marker = "function applyLanguage("

	at := strings.Index(js, marker)
	if at < 0 {
		t.Fatal("app.js no longer contains applyLanguage; this test is reading nothing")
	}

	rest := js[at+len(marker):]

	end := regexp.MustCompile(`(?m)^(async function |function |const |// -----)`).
		FindStringIndex(rest)
	if end == nil {
		return js[at:]
	}

	return js[at : at+len(marker)+end[0]]
}
