//go:build integration

package integration

import (
	"net/http"
	"testing"
)

type entryVersion struct {
	Date          string  `json:"date"`
	DurationHours float64 `json:"durationHours"`
	ProjectID     *uint   `json:"projectId"`
	Description   *string `json:"description"`
}

// Every way an entry is corrected keeps what it said before - the form and a
// transfer to another project alike - while a save that changes nothing keeps
// nothing; the history is its owner's alone, and goes with the entry, on every
// database.
func TestAnEntryKeepsWhatItSaidBeforeEachCorrection(t *testing.T) {
	t.Parallel()

	a, admin, worker := startWithWorker(t)

	var entry struct {
		ID uint `json:"id"`
	}

	worker.must(worker.api(http.MethodPost, "/timesheets", map[string]any{
		"date": "2026-08-03", "durationHours": 2.5,
	}), http.StatusCreated, http.StatusOK).Data(t, &entry)

	entryPath := path("/timesheets/", entry.ID)

	worker.must(worker.api(http.MethodPut, entryPath, map[string]any{"durationHours": 3}), http.StatusOK)
	worker.must(worker.api(http.MethodPut, entryPath, map[string]any{"durationHours": 3}), http.StatusOK)

	var project struct {
		ID uint `json:"id"`
	}

	worker.must(worker.api(http.MethodPost, "/projects", map[string]any{
		"name": "Portal", "startDate": "2026-08-01",
	}), http.StatusCreated, http.StatusOK).Data(t, &project)

	worker.must(worker.api(http.MethodPost, entryPath+"/transfer", map[string]any{"projectId": project.ID}),
		http.StatusOK, http.StatusCreated)

	var history listOf[entryVersion]

	worker.must(worker.api(http.MethodGet, entryPath+"/revisions", nil), http.StatusOK).Data(t, &history)

	if len(history.Items) != 2 {
		t.Fatalf("a correction, a save of the same and a transfer left %+v, want two earlier versions", history.Items)
	}

	if moved := history.Items[0]; moved.DurationHours != 3 || moved.ProjectID != nil {
		t.Errorf("the version the transfer replaced reads %+v, want three hours on no project", moved)
	}

	if first := history.Items[1]; first.DurationHours != 2.5 || first.Date != "2026-08-03" {
		t.Errorf("the version as booked reads %+v, want 2.5 hours on the 3rd", first)
	}

	stranger := a.signInAsUser(admin, "Sven", "sven@example.com")

	if got := stranger.api(http.MethodGet, entryPath+"/revisions", nil).Status; got != http.StatusNotFound {
		t.Errorf("somebody else asking for the history was answered %d, want 404", got)
	}

	worker.must(worker.api(http.MethodDelete, entryPath, nil), http.StatusOK, http.StatusNoContent)

	if left := a.Rows(t, "timesheet_revisions", "timesheet_id = ?", entry.ID); left != 0 {
		t.Errorf("%d version(s) outlived the entry they belonged to", left)
	}
}

// An account's entries take their history with them when it is deleted, on every
// database: PostgreSQL refuses the deletion outright if a version is left pointing
// at the account or at one of its entries.
func TestDeletingAnAccountTakesItsEntriesHistoryWithIt(t *testing.T) {
	t.Parallel()

	a, admin, worker := startWithWorker(t)

	var me struct {
		User userResponse `json:"user"`
	}

	worker.must(worker.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &me)

	var entry struct {
		ID uint `json:"id"`
	}

	worker.must(worker.api(http.MethodPost, "/timesheets", map[string]any{
		"date": "2026-08-03", "durationHours": 2.5,
	}), http.StatusCreated, http.StatusOK).Data(t, &entry)

	worker.must(worker.api(http.MethodPut, path("/timesheets/", entry.ID), map[string]any{"durationHours": 4}),
		http.StatusOK)

	// Confirmed, as the deletion of an account with recorded time has to be.
	admin.must(admin.api(http.MethodDelete, path("/users/", me.User.ID, "?purge=true"), nil),
		http.StatusOK, http.StatusNoContent)

	if left := a.Rows(t, "timesheet_revisions", "user_id = ?", me.User.ID); left != 0 {
		t.Errorf("%d version(s) outlived the account they belonged to", left)
	}
}
