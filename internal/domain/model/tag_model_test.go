package model_test

import (
	"slices"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// A description names its activities where a word starts with #: once each,
// lower-cased, in order - and a ticket number, a reference inside a word and the
// # of a language's name are not activities.
func TestADescriptionNamesItsActivities(t *testing.T) {
	for description, want := range map[string][]string{
		"#Meeting with the team":                  {"meeting"},
		"Sprint review #review #meeting #Review":  {"review", "meeting"},
		"Fixed #123, abc#45 and issue#abc":        nil,
		"Wrote C# for the parser":                 nil,
		"(#Übergabe), #code-review; #ops_on-call": {"übergabe", "code-review", "ops_on-call"},
		"#2fa rollout":                            {"2fa"},
		"#":                                       nil,
		"no tags here":                            nil,
	} {
		if got := model.Tags(description); !slices.Equal(got, want) {
			t.Errorf("%q names %v, want %v", description, got, want)
		}
	}
}
