package web_test

import (
	"html"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// A translation key says one thing.
//
// One translation serves every place a key is used, so a key written over two
// different Englishes is translated as one of them in both. Three had drifted that
// way: the spreadsheet cards' "cancel" borrowed the forms' "cancel editing", and a
// German import card offered "Bearbeiten abbrechen" with nothing being edited; a
// passkey that was not created and one that was not used shared a sentence, so a
// German reader who stopped adding one was told it "wurde nicht verwendet"; and the
// passkeys table took the add form's "Name for this device", which German renders
// as "Name für dieses Gerät" above every device's name.
//
// The English is read where it is written: an element's own text for data-i18n,
// the fallback of a t('key', '...') lookup, and the text of an element the script
// builds with a data-i18n of its own.
func TestOneKeySaysOneThing(t *testing.T) {
	said := map[string]map[string]bool{}

	note := func(key, english string) {
		english = strings.Join(strings.Fields(html.UnescapeString(english)), " ")
		if english == "" {
			return
		}

		if said[key] == nil {
			said[key] = map[string]bool{}
		}

		said[key][english] = true
	}

	for _, m := range regexp.MustCompile(`data-i18n="([^"]+)"[^>]*>([^<]*)`).FindAllStringSubmatch(asset(t, "/"), -1) {
		note(m[1], m[2])
	}

	code := withoutLineComments(asset(t, "/app.js"))

	for _, pattern := range []string{
		`\bt\(\s*'([^']+)'\s*,\s*'((?:[^'\\]|\\.)*)'`,
		`'data-i18n':\s*'([^']+)'[^}]*?\btext:\s*'((?:[^'\\]|\\.)*)'`,
	} {
		for _, m := range regexp.MustCompile(pattern).FindAllStringSubmatch(code, -1) {
			note(m[1], strings.ReplaceAll(m[2], `\'`, `'`))
		}
	}

	if len(said) < 100 {
		t.Fatalf("read the English of only %d keys; this case is no longer reading the assets", len(said))
	}

	var keys []string

	for key, englishes := range said {
		if len(englishes) > 1 {
			keys = append(keys, key)
		}
	}

	sort.Strings(keys)

	for _, key := range keys {
		var versions []string

		for english := range said[key] {
			versions = append(versions, strconv.Quote(english))
		}

		sort.Strings(versions)

		t.Errorf("%s is written over %d different Englishes, and translated as one of them "+
			"in all: %s", key, len(versions), strings.Join(versions, ", "))
	}
}
