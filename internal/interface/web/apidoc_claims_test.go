package web_test

import (
	"encoding/json"
	"regexp"
	"slices"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// operationDescriptions reads every operation's description out of the API
// document, keyed by its method and path.
func operationDescriptions(t *testing.T) map[string]string {
	t.Helper()

	var document struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}

	if err := json.Unmarshal([]byte(asset(t, "/openapi.json")), &document); err != nil {
		t.Fatalf("the served openapi.json is not valid JSON: %v", err)
	}

	descriptions := map[string]string{}

	for path, item := range document.Paths {
		for method, raw := range item {
			// A path item carries its parameters beside its operations.
			if !slices.Contains([]string{"get", "put", "post", "delete", "patch"}, method) {
				continue
			}

			var operation struct {
				Description string `json:"description"`
			}

			if err := json.Unmarshal(raw, &operation); err != nil {
				t.Fatalf("%s %s is not an operation: %v", method, path, err)
			}

			descriptions[method+" "+path] = operation.Description
		}
	}

	return descriptions
}

// The API document describes no state a time entry could be in.
//
// An entry has no status and no approval path, and the domain rules say why - but
// the statistics description went on promising totals "per workflow state" and
// leaving out "rejected entries" long after the review step was taken out, because
// nothing read it against the code. A description advertising a state is how
// somebody comes to build one back.
func TestNoDescriptionGivesAnEntryAState(t *testing.T) {
	state := regexp.MustCompile(`(?i)workflow|approv|(rejected|approved|submitted) entr`)

	for operation, description := range operationDescriptions(t) {
		if found := state.FindString(description); found != "" {
			t.Errorf("%s says %q, and a time entry has no state", operation, found)
		}
	}
}

// Every right a description says an operation needs is one that exists.
//
// The statistics description sent its reader to "reports:read" for the project
// report, a right that had been retired - the report takes reports:read:own and
// answers with nobody's hours but the caller's. A right named as a requirement is
// a claim somebody acts on, so it is held to the model; one named as history ("there
// was ...") is not a requirement and is left alone.
func TestEveryRightADescriptionRequiresExists(t *testing.T) {
	needs := regexp.MustCompile("(?i)\\bneeds(?: only)? `?([a-z]+:[a-z]+(?::[a-z]+)?)`?")
	known := model.AllPermissions()

	required := 0

	for operation, description := range operationDescriptions(t) {
		for _, match := range needs.FindAllStringSubmatch(description, -1) {
			required++

			if !slices.Contains(known, match[1]) {
				t.Errorf("%s says it needs %q, which is not a right this application has", operation, match[1])
			}
		}
	}

	if required == 0 {
		t.Fatal("no description says what it needs; this case is reading nothing")
	}
}
