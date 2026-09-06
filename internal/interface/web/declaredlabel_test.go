package web_test

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// An element that declares a translation key is not written over with plain text.
//
// applyLanguage translates every [data-i18n] node from its key, so an element
// that says one of two things - a form title that creates or corrects, a button
// that pauses or resumes, one that exports or says it is preparing - has the
// declared message put back over the shown one at the next language change. The
// screen then disagrees with itself, and where the two messages mean different
// actions it disagrees about what a press will do.
//
// swapTheLabel is the way to say it: it writes the words, declares the key they
// came from, and puts the English source beside it so the way back to English
// finds the right one.
//
// Six sites were found this way and all six are fixed, so the expectation is
// none. A new one is not necessarily wrong - an element whose text is entirely
// the script's should not be carrying a key at all - but it is always one of
// those two things, and both are worth being told about.
func TestNoDeclaredLabelIsWrittenOverWithPlainText(t *testing.T) {
	html := asset(t, "/")
	js := asset(t, "/app.js")

	declared := map[string]string{}

	for _, tag := range regexp.MustCompile(`<[^>]+>`).FindAllString(html, -1) {
		id := regexp.MustCompile(`id="([^"]+)"`).FindStringSubmatch(tag)
		key := regexp.MustCompile(`data-i18n="([^"]+)"`).FindStringSubmatch(tag)

		if id != nil && key != nil {
			declared[id[1]] = key[1]
		}
	}

	if len(declared) < 20 {
		t.Fatalf("found %d elements carrying both an id and a key, which is too few "+
			"to be reading the markup correctly", len(declared))
	}

	var written []string

	for id, key := range declared {
		// The script reaching for it by id, and what it does on that line.
		pattern := regexp.MustCompile(`\$\('#` + regexp.QuoteMeta(id) + `'\)([^\n]*)`)

		for _, match := range pattern.FindAllStringSubmatch(js, -1) {
			line := match[1]

			if !strings.Contains(line, ".textContent") || !strings.Contains(line, "=") {
				continue
			}

			written = append(written, id+" (declares "+key+")")

			break
		}
	}

	sort.Strings(written)

	for _, one := range written {
		t.Errorf("#%s is written over with textContent while declaring a key, so a "+
			"language change puts the declared message back over the shown one. "+
			"swapTheLabel writes the words and the key together", one)
	}
}
