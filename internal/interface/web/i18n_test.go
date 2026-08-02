package web_test

import (
	"net/http"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The interface is written in English and every other language is a dictionary
// layered over it. That is what makes English the fallback: a key nobody has
// translated yet still renders, in English, instead of falling through to a
// language the reader may not know.
//
// These tests read the shipped assets rather than a copy, so they fail if the
// arrangement is ever undone by hand.

func asset(t *testing.T, path string) string {
	t.Helper()

	rec := get(t, http.MethodGet, path)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for %s, got %d", path, rec.Code)
	}

	return rec.Body.String()
}

var (
	// A dictionary: `  <lang>: {` up to the matching `  },`. The empty form
	// `{}` is matched first, or the search would run past it and swallow the
	// next language's entries.
	dictPattern = regexp.MustCompile(`(?s)\n  ([a-z]{2}): \{(\}|.*?\n  \}),`)

	// One entry, whose value may be several literals joined by "+".
	entryPattern = regexp.MustCompile(`(?s)'([^']+)':\s*((?:'(?:[^'\\]|\\.)*'\s*\+?\s*)+),`)
)

// dictionaries reads the translation tables out of app.js.
func dictionaries(t *testing.T) map[string]map[string]string {
	t.Helper()

	js := asset(t, "/app.js")

	start := strings.Index(js, "const TRANSLATIONS = {")
	if start < 0 {
		t.Fatal("app.js no longer declares TRANSLATIONS")
	}

	out := map[string]map[string]string{}

	for _, dict := range dictPattern.FindAllStringSubmatch(js[start:], -1) {
		entries := map[string]string{}
		for _, entry := range entryPattern.FindAllStringSubmatch(dict[2], -1) {
			entries[entry[1]] = entry[2]
		}

		out[dict[1]] = entries
	}

	if len(out) == 0 {
		t.Fatal("no dictionaries found; the TRANSLATIONS layout changed")
	}

	return out
}

// markupKeys collects every translation key the page markup refers to.
func markupKeys(t *testing.T) []string {
	t.Helper()

	html := asset(t, "/")

	seen := map[string]bool{}

	for _, attribute := range []string{"data-i18n", "data-i18n-placeholder", "data-i18n-aria"} {
		pattern := regexp.MustCompile(attribute + `="([^"]+)"`)
		for _, m := range pattern.FindAllStringSubmatch(html, -1) {
			seen[m[1]] = true
		}
	}

	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}

// English lives in the markup, so an "en" dictionary would be a second source
// of truth that could drift from what the page actually says.
func TestEnglishIsTheSourceNotATranslation(t *testing.T) {
	dicts := dictionaries(t)

	english, ok := dicts["en"]
	if !ok {
		t.Fatal("expected an en entry in TRANSLATIONS, even if empty")
	}

	if len(english) != 0 {
		t.Errorf("en must stay empty because the markup already carries English, "+
			"but it holds %d entries", len(english))
	}
}

// A key the markup uses but a translation lacks shows English there. That is
// the intended fallback, not a licence to leave gaps.
func TestEveryTranslationCoversTheMarkup(t *testing.T) {
	dicts := dictionaries(t)
	keys := markupKeys(t)

	if len(keys) == 0 {
		t.Fatal("no translation keys found in the markup")
	}

	for language, dict := range dicts {
		if language == "en" {
			continue
		}

		var missing []string

		for _, key := range keys {
			if _, ok := dict[key]; !ok {
				missing = append(missing, key)
			}
		}

		if len(missing) > 0 {
			t.Errorf("%s is missing %d key(s), which would render in English: %v",
				language, len(missing), missing)
		}
	}
}

// A dictionary entry nobody looks up is dead weight that outlives the text it
// once translated.
func TestNoTranslationIsUnused(t *testing.T) {
	dicts := dictionaries(t)
	js := asset(t, "/app.js")
	html := asset(t, "/")

	for language, dict := range dicts {
		if language == "en" {
			continue
		}

		var unused []string

		for key := range dict {
			// Either the markup names it, or code looks it up via t().
			if strings.Contains(html, `"`+key+`"`) || strings.Contains(js, `t('`+key+`',`) {
				continue
			}

			unused = append(unused, key)
		}

		sort.Strings(unused)

		if len(unused) > 0 {
			t.Errorf("%s has %d entry(s) nothing refers to: %v", language, len(unused), unused)
		}
	}
}

// The fallbacks handed to t() are what shows when a key is absent, so they must
// be the English text rather than another language's.
func TestCodeFallbacksAreEnglish(t *testing.T) {
	js := asset(t, "/app.js")

	start := strings.Index(js, "const TRANSLATIONS = {")
	end := strings.Index(js[start:], "\n};")

	if start < 0 || end < 0 {
		t.Fatal("could not locate the TRANSLATIONS block")
	}

	// Everything except the dictionaries themselves, which are allowed to hold
	// other languages.
	code := js[:start] + js[start+end:]

	// Accented Latin, Greek or Cyrillic letters mean another language leaked
	// into the source. Typographic punctuation is not included: an ellipsis or
	// an em dash is ordinary in English too.
	suspicious := regexp.MustCompile(
		`t\('[^']+',\s*'[^']*[\x{00C0}-\x{024F}\x{0370}-\x{04FF}][^']*'\)`)
	if found := suspicious.FindAllString(code, -1); found != nil {
		t.Errorf("these t() fallbacks do not look like English: %v", found)
	}
}
