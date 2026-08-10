//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// A refusal has to say which rule was broken, not only say so in English.
//
// The server writes its messages where the rule is enforced, which is right for
// the log and wrong for the person who tripped over it: the interface showed them
// "an approved timesheet can no longer be edited" whatever language they had
// chosen. The reason now travels as a code with the values the sentence
// interpolated, and the interface looks the sentence up in the reader's language.
//
// Proved on the wire rather than in the browser, because the wire is the contract:
// the code has to survive GoFr's error rendering, which is where a field that is
// not part of its own error types would be dropped.

// refusal is the error object as it reaches a client.
type refusal struct {
	Message string `json:"message"`
	Code    string `json:"code"`
	Values  []any  `json:"values"`
}

func refusalOf(t *testing.T, r response) refusal {
	t.Helper()

	var body struct {
		Error refusal `json:"error"`
	}

	if err := json.Unmarshal([]byte(r.Body), &body); err != nil {
		t.Fatalf("the error body is not JSON: %v\n%s", err, r.Body)
	}

	return body.Error
}

// A conflict, which GoFr renders through a local error type.
func TestAConflictCarriesItsReasonAndValues(t *testing.T) {
	_, _, worker := startWithWorker(t)

	// A project with an entry on it cannot be deleted, and the refusal says how
	// many entries are in the way - the number the German sentence needs.
	var project projectResponse
	worker.must(worker.api(http.MethodPost, "/projects", map[string]any{
		"name": "Busy", "startDate": "2026-08-01",
	}), http.StatusCreated, http.StatusOK).Data(t, &project)

	worker.must(worker.api(http.MethodPost, "/timesheets", map[string]any{
		"date": "2026-08-03", "durationHours": 2, "projectId": project.ID,
	}), http.StatusCreated, http.StatusOK)

	refused := worker.api(http.MethodDelete, path("/projects/", project.ID), nil)
	if refused.Status != http.StatusConflict {
		t.Fatalf("deleting a project with entries answered %d, want 409: %s",
			refused.Status, refused.Body)
	}

	reason := refusalOf(t, refused)

	if reason.Code != "projectHasEntries" {
		t.Errorf("the refusal is coded %q, want projectHasEntries", reason.Code)
	}

	// One value, the count. JSON has one number type, so 1 arrives as 1.0.
	if len(reason.Values) != 1 {
		t.Fatalf("the refusal carries %d value(s), want 1: %v", len(reason.Values), reason.Values)
	}

	if count, ok := reason.Values[0].(float64); !ok || count != 1 {
		t.Errorf("the refusal counts %v entries, want 1", reason.Values[0])
	}

	// The English message stays: it is what the log records, and what a client
	// without the sentence falls back to.
	if !strings.Contains(reason.Message, "still has 1 time entries") {
		t.Errorf("the message no longer explains itself: %q", reason.Message)
	}
}

// Invalid input, which is the other rendering path - and the one that used to
// wrap the sentence in GoFr's "'1' invalid parameter(s):".
func TestInvalidInputCarriesItsReasonAndValues(t *testing.T) {
	// Somebody who works here, because the two figures are a time figure each and
	// belong to the person they are about. The administrator is refused this route
	// before the request is even read, so it could never reach the rule under test.
	_, _, worker := startWithWorker(t)

	var me struct {
		User userResponse `json:"user"`
	}

	worker.must(worker.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &me)

	// A daily target above the daily maximum, which is refused with both figures.
	refused := worker.api(http.MethodPut, path("/users/", me.User.ID, "/working-times"),
		map[string]any{"dailyTargetHours": 9, "maxDailyHours": 8})

	if refused.Status != http.StatusBadRequest {
		t.Fatalf("a target above the maximum answered %d, want 400: %s",
			refused.Status, refused.Body)
	}

	reason := refusalOf(t, refused)

	if reason.Code != "targetOverMaximum" {
		t.Errorf("the refusal is coded %q, want targetOverMaximum", reason.Code)
	}

	if len(reason.Values) != 2 {
		t.Fatalf("the refusal carries %d value(s), want the target and the maximum: %v",
			len(reason.Values), reason.Values)
	}

	target, _ := reason.Values[0].(float64)
	maximum, _ := reason.Values[1].(float64)

	if target != 9 || maximum != 8 {
		t.Errorf("the refusal carries %v and %v, want 9 and 8", target, maximum)
	}

	// The sentence itself, no longer behind a wrapper that counted parameters.
	if strings.Contains(reason.Message, "invalid parameter") {
		t.Errorf("the reason is still wrapped in a parameter count: %q", reason.Message)
	}

	if !strings.Contains(reason.Message, "cannot exceed") {
		t.Errorf("the message no longer explains itself: %q", reason.Message)
	}
}

// An error nobody has annotated keeps exactly the body it had, so adding the
// mechanism cannot have changed what an unannotated refusal looks like.
func TestAnUnannotatedRefusalIsUnchanged(t *testing.T) {
	_, _, worker := startWithWorker(t)

	// A field-level rejection, which travels as a list of field names rather than
	// as a coded sentence.
	refused := worker.api(http.MethodPost, "/timesheets", map[string]any{
		"date": "2026-08-03", "durationHours": -1,
	})

	if refused.Status != http.StatusBadRequest {
		t.Fatalf("negative hours answered %d, want 400: %s", refused.Status, refused.Body)
	}

	if reason := refusalOf(t, refused); reason.Code != "" {
		t.Errorf("a field rejection now carries the code %q; it names fields instead",
			reason.Code)
	}

	var body struct {
		Error struct {
			Param []string `json:"param"`
		} `json:"error"`
	}

	if err := json.Unmarshal([]byte(refused.Body), &body); err != nil {
		t.Fatalf("the error body is not JSON: %v", err)
	}

	if len(body.Error.Param) == 0 {
		t.Errorf("the rejection no longer says which field: %s", refused.Body)
	}
}
