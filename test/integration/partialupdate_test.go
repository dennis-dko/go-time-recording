//go:build integration

package integration

import (
	"net/http"
	"testing"
)

// A correction over HTTP says three things: a field left out, or sent as null,
// stays as it is; a value sets it; and the field's own empty form clears it - 0
// for an entry's project, an empty string for a description or an end date.
//
// The services were tested for the three states, and nothing held them where a
// client meets them: the translation from JSON to the command sits in the
// handlers, and the convention a client is likeliest to bring - JSON Merge
// Patch, where null removes a field - is the one read here as "leave it".
func TestACorrectionLeavesSetsAndClearsOverHTTP(t *testing.T) {
	t.Parallel()

	_, _, worker := startWithWorker(t)

	var project struct {
		ID      uint    `json:"id"`
		EndDate *string `json:"endDate"`
	}

	worker.must(worker.api(http.MethodPost, "/projects", map[string]any{
		"name": "Kept", "startDate": "2026-08-01", "endDate": "2026-12-31",
	}), http.StatusCreated, http.StatusOK).Data(t, &project)

	var entry timesheetResponse

	worker.must(worker.api(http.MethodPost, "/timesheets", map[string]any{
		"date": "2026-08-02", "durationHours": 3, "description": "Kept too", "projectId": project.ID,
	}), http.StatusCreated, http.StatusOK).Data(t, &entry)

	worker.must(worker.api(http.MethodPut, path("/timesheets/", entry.ID), map[string]any{
		"projectId": nil, "description": nil, "durationHours": 4,
	}), http.StatusOK).Data(t, &entry)

	if entry.ProjectID == nil || *entry.ProjectID != project.ID {
		t.Errorf("a projectId of null took the entry off its project: %v", entry.ProjectID)
	}

	if entry.Description == nil || *entry.Description != "Kept too" {
		t.Errorf("a description of null changed it: %v", entry.Description)
	}

	worker.must(worker.api(http.MethodPut, path("/timesheets/", entry.ID), map[string]any{
		"projectId": 0, "description": "",
	}), http.StatusOK).Data(t, &entry)

	if entry.ProjectID != nil {
		t.Errorf("a projectId of 0 left the entry on project %d", *entry.ProjectID)
	}

	if entry.Description != nil && *entry.Description != "" {
		t.Errorf("an empty description kept %q", *entry.Description)
	}

	worker.must(worker.api(http.MethodPut, path("/projects/", project.ID), map[string]any{
		"endDate": nil, "name": "Kept, renamed",
	}), http.StatusOK).Data(t, &project)

	if project.EndDate == nil || *project.EndDate == "" {
		t.Error("an endDate of null took the end date off")
	}

	worker.must(worker.api(http.MethodPut, path("/projects/", project.ID), map[string]any{
		"endDate": "",
	}), http.StatusOK).Data(t, &project)

	if project.EndDate != nil && *project.EndDate != "" {
		t.Errorf("an empty endDate kept %q", *project.EndDate)
	}
}
