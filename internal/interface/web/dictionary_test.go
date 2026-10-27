package web_test

import (
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/interface/web"
)

// What the binary reads out of the page's dictionary is the whole dictionary, as
// these tests read it: no entry dropped, and each value the text the page has.
//
// Two readings of one table, so this is what keeps them one: the terminal client
// says a refusal from the production reading, and the checks that every key is
// translated run on the other.
func TestTheDictionaryIsReadWhole(t *testing.T) {
	read := web.Dictionary("de")
	tested := dictionaries(t)["de"]

	if len(tested) < 1000 {
		t.Fatalf("the tests read %d German entries; the reading has stopped finding them", len(tested))
	}

	for key, raw := range tested {
		value, ok := read[key]
		if !ok {
			t.Errorf("%s is in the page's German and not in what the binary reads", key)

			continue
		}

		// With nothing escaped, which the table never is today, a value is the
		// text between its quotes - several literals' worth where it is joined.
		if !strings.Contains(raw, `\`) {
			var want strings.Builder
			for i, part := range strings.Split(raw, "'") {
				if i%2 == 1 {
					want.WriteString(part)
				}
			}

			if value != want.String() {
				t.Errorf("%s is read as %q, want %q", key, value, want.String())
			}
		}
	}

	for key := range read {
		if _, ok := tested[key]; !ok {
			t.Errorf("the binary reads %s, which the page's German does not have", key)
		}
	}

	if english := web.Dictionary("en"); len(english) != 0 {
		t.Errorf("English is read as %d entries; it is the source and has none", len(english))
	}
}

// REFUSAL_VALUES is read whole: every code the page writes values of, and with
// which of its functions.
func TestTheValuesOfARefusalAreReadAsThePageWritesThem(t *testing.T) {
	named := web.RefusalValues()

	js := asset(t, "/app.js")

	start := strings.Index(js, "const REFUSAL_VALUES = {")
	if start < 0 {
		t.Fatal("app.js no longer declares REFUSAL_VALUES")
	}

	block, _, _ := strings.Cut(js[start:], "\n};")
	codes := strings.Count(block, ": {")

	if codes < 1 || len(named) != codes {
		t.Fatalf("REFUSAL_VALUES has %d codes and %d were read: %v", codes, len(named), named)
	}

	if kind := named["projectClosedForBooking"][1]; kind != "statusName" {
		t.Errorf("the second value of projectClosedForBooking is read as written by %q, want statusName", kind)
	}
}
