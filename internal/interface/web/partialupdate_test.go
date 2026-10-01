package web_test

import (
	"encoding/json"
	"strings"
	"testing"
)

// The two corrections that can empty a field say how, in the document a client
// reads.
//
// Both read a field left out, or sent as null, as "leave it", and each has its
// own empty form for "clear it": 0 takes an entry off its project, an empty
// string empties its description, and an empty endDate takes a project's end
// date off. None of it was written down, and a client following JSON Merge Patch
// sends null to remove a field - answered 200, with nothing changed.
// TestACorrectionLeavesSetsAndClearsOverHTTP holds the behaviour; this holds that
// the document says it.
func TestAPartialUpdateSaysHowToLeaveAndHowToClear(t *testing.T) {
	var document struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}

	if err := json.Unmarshal([]byte(asset(t, "/openapi.json")), &document); err != nil {
		t.Fatalf("the served openapi.json is not valid JSON: %v", err)
	}

	for path, phrases := range map[string][]string{
		"/timesheets/{id}": {"left out or sent as null", "`projectId` of 0", "empty `description`"},
		"/projects/{id}":   {"left out or sent as null", "empty `endDate`"},
	} {
		var put struct {
			Description string `json:"description"`
		}

		if err := json.Unmarshal(document.Paths[path]["put"], &put); err != nil {
			t.Fatalf("PUT %s is not described: %v", path, err)
		}

		description := put.Description

		for _, phrase := range phrases {
			if !strings.Contains(description, phrase) {
				t.Errorf("PUT %s does not say %q: %q", path, phrase, description)
			}
		}
	}
}
