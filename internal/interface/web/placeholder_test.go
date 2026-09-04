package web_test

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

var (
	// A quoted literal, possibly built from several joined with +, which is how
	// the longer sentences are written in this file.
	joinedLiteral = `((?:'(?:[^'\\]|\\.)*'\s*\+?\s*)+)`

	quotedPart = regexp.MustCompile(`'((?:[^'\\]|\\.)*)'`)

	// The two ways code names a key: t(key, english) and swapTheLabel(node, key,
	// english), which is a lookup that also declares the key on the element.
	lookupPatterns = []*regexp.Regexp{
		regexp.MustCompile(`t\(\s*'([a-zA-Z0-9._:-]+)'\s*,\s*` + joinedLiteral + `\s*\)`),
		regexp.MustCompile(`swapTheLabel\([^,]+,\s*'([a-zA-Z0-9._:-]+)'\s*,\s*` +
			joinedLiteral + `\s*\)`),
	}

	placeholderPattern = regexp.MustCompile(`\{[0-9a-z]+\}`)
)

// A translation carries the same values its English does.
//
// The messages that interpolate do it by replacing {0} in whatever the
// dictionary returned, so a German string that has lost the placeholder silently
// drops the value, and one that has gained a placeholder its English lacks shows
// the braces to an English reader. Neither fails anything: the sentence still
// renders, just without the number, the name or the version in it.
//
// role.edit was that: "Edit role" in English against "Rolle „{0}" bearbeiten" in
// German, on the one screen where a single form serves every role and the
// heading is what says which one is open. The name reached German readers and
// not English ones for as long as both existed.
func TestEveryTranslationCarriesTheSameValuesAsItsEnglish(t *testing.T) {
	js := asset(t, "/app.js")

	english := map[string]string{}

	for _, pattern := range lookupPatterns {
		for _, match := range pattern.FindAllStringSubmatch(js, -1) {
			english[match[1]] = joinParts(match[2])
		}
	}

	if len(english) < 100 {
		t.Fatalf("found %d English fallbacks in app.js, which is too few to be "+
			"reading it correctly", len(english))
	}

	compared := 0

	for language, dict := range dictionaries(t) {
		if language == "en" {
			continue
		}

		for key, translated := range dict {
			source, known := english[key]
			if !known {
				continue
			}

			compared++

			want := placeholdersIn(source)
			got := placeholdersIn(translated)

			if strings.Join(want, ",") == strings.Join(got, ",") {
				continue
			}

			t.Errorf("%s carries %v where its English carries %v.\n  en: %s\n  %s: %s",
				key, got, want, source, language, translated)
		}
	}

	if compared < 100 {
		t.Fatalf("compared %d keys, which is too few to be reading both sides", compared)
	}

	t.Logf("compared %d translations against their English", compared)
}

// joinParts turns 'a' + 'b' into ab.
func joinParts(blob string) string {
	var out strings.Builder

	for _, match := range quotedPart.FindAllStringSubmatch(blob, -1) {
		out.WriteString(match[1])
	}

	return out.String()
}

// placeholdersIn returns the distinct {0}-style values a sentence interpolates,
// in a fixed order so two sentences can be compared.
func placeholdersIn(sentence string) []string {
	seen := map[string]bool{}

	var found []string

	for _, match := range placeholderPattern.FindAllString(sentence, -1) {
		if seen[match] {
			continue
		}

		seen[match] = true

		found = append(found, match)
	}

	sort.Strings(found)

	return found
}
