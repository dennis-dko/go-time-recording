package web_test

import (
	"regexp"
	"strings"
	"testing"
)

// Every request the page makes goes through reach, which is where a request that
// never arrived is put into words.
//
// fetch failing before there is an answer throws the browser's own exception,
// worded in the browser's language and its own way - "Failed to fetch" in one,
// "NetworkError when attempting to fetch resource." in another. api() rethrew it
// untouched, and so did the download and both imports, which ask fetch directly
// because they need a blob or a multipart body: four places, and a fifth would
// have had to remember the same thing. reach is the one place, and this is what
// makes it the only one.
func TestEveryRequestGoesThroughReach(t *testing.T) {
	script := readAsset(t, "app.js")

	call := regexp.MustCompile(`(^|[^.\w])fetch\(`)

	var found []int

	for i, line := range strings.Split(script, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*") {
			continue
		}

		if call.MatchString(line) {
			found = append(found, i+1)
		}
	}

	if !strings.Contains(script, "async function reach(") {
		t.Fatal("app.js has no reach, so nothing puts a request that never arrived into words")
	}

	if len(found) != 1 {
		t.Errorf("fetch is called on %d lines of app.js (%v); it belongs in reach alone", len(found), found)
	}
}
