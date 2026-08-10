//go:build integration

package integration

import (
	"net/http"
	"strings"
	"testing"
)

// An entry belongs to the person who recorded it.
//
// Now that everyone keeps their own hours and there is nobody to review them, that
// is the whole rule - and it was enforced in three places out of four. Create,
// Update and Delete each checked whose entry they were touching. Transfer checked
// only that the caller held the permission, and editing checked only that the
// project it was being moved to existed.
//
// These are API tests rather than unit tests on purpose: the checks live in the
// handlers, and what matters is what the endpoint answers, not what a function
// returns.

// twoColleagues signs in an administrator and two ordinary accounts.
func twoColleagues(t *testing.T) (*client, *client, *client) {
	t.Helper()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	return admin,
		a.signInAsUser(admin, "Anna", "anna@example.com"),
		a.signInAsUser(admin, "Bert", "bert@example.com")
}

// entryOf books an entry for the caller and returns it.
func entryOf(t *testing.T, c *client, body map[string]any) timesheetResponse {
	t.Helper()

	var entry timesheetResponse

	c.must(c.api(http.MethodPost, "/timesheets", body),
		http.StatusCreated, http.StatusOK).Data(t, &entry)

	return entry
}

// Transferring somebody else's entry is refused, and tells the caller nothing
// about it.
//
// The employee role holds timesheets:transfer, so before this the permission alone
// was enough: any employee could move a colleague's hours onto another project,
// changing that colleague's totals, and read the entry's date, hours and free-text
// description out of the response. Entry ids are small and sequential, so walking
// somebody else's week cost nothing.
func TestTransferringSomebodyElsesEntryIsRefused(t *testing.T) {
	_, anna, bert := twoColleagues(t)

	var shared projectResponse
	anna.must(anna.api(http.MethodPost, "/projects", map[string]any{
		"name": "Shared work", "startDate": "2026-08-01",
	}), http.StatusCreated, http.StatusOK).Data(t, &shared)

	// Bert's entry, with something in the description worth not leaking.
	bertsEntry := entryOf(t, bert, map[string]any{
		"date": "2026-08-04", "durationHours": 3, "description": "a private note",
	})

	answer := anna.api(http.MethodPost, path("/timesheets/", bertsEntry.ID)+"/transfer",
		map[string]any{"projectId": shared.ID})

	if answer.Status != http.StatusForbidden {
		t.Errorf("transferring a colleague's entry answered %d, want 403", answer.Status)
	}

	// And the refusal carries none of it. A 403 that quotes the description would
	// have leaked exactly what the check is there to protect.
	if strings.Contains(string(answer.Body), "a private note") {
		t.Errorf("the refusal repeats the entry's description: %.200q", answer.Body)
	}

	// Bert's entry is where he left it.
	var still timesheetResponse
	bert.must(bert.api(http.MethodGet, path("/timesheets/", bertsEntry.ID), nil),
		http.StatusOK).Data(t, &still)

	if still.ProjectID != nil {
		t.Errorf("the entry was moved onto project %d anyway", *still.ProjectID)
	}
}

// Transferring one's own entry still works, which is what the permission is for.
func TestTransferringYourOwnEntryStillWorks(t *testing.T) {
	_, anna, _ := twoColleagues(t)

	var shared projectResponse
	anna.must(anna.api(http.MethodPost, "/projects", map[string]any{
		"name": "Shared work", "startDate": "2026-08-01",
	}), http.StatusCreated, http.StatusOK).Data(t, &shared)

	own := entryOf(t, anna, map[string]any{"date": "2026-08-04", "durationHours": 2})

	var moved timesheetResponse
	anna.must(anna.api(http.MethodPost, path("/timesheets/", own.ID)+"/transfer",
		map[string]any{"projectId": shared.ID}), http.StatusCreated, http.StatusOK).
		Data(t, &moved)

	if moved.ProjectID == nil || *moved.ProjectID != shared.ID {
		t.Errorf("the entry was not moved onto the project: %+v", moved)
	}
}

// An entry cannot be handed to a colleague by naming them in an edit.
//
// The edit checked whose entry it was, and then let the body say whose it should
// become - so somebody who may only write their own could push hours onto an
// account that is not theirs to book for.
func TestAnEntryCannotBeHandedToAColleague(t *testing.T) {
	admin, anna, bert := twoColleagues(t)

	var bertAccount []struct {
		ID    uint   `json:"id"`
		Email string `json:"email"`
	}

	listed := admin.must(admin.api(http.MethodGet, "/users", nil), http.StatusOK)

	var people struct {
		Items []struct {
			ID    uint   `json:"id"`
			Email string `json:"email"`
		} `json:"items"`
	}

	listed.Data(t, &people)
	bertAccount = people.Items

	var bertID uint

	for _, person := range bertAccount {
		if person.Email == "bert@example.com" {
			bertID = person.ID
		}
	}

	if bertID == 0 {
		t.Fatal("Bert is not in the account list")
	}

	own := entryOf(t, anna, map[string]any{"date": "2026-08-05", "durationHours": 2})

	answer := anna.api(http.MethodPut, path("/timesheets/", own.ID),
		map[string]any{"userId": bertID})

	if answer.Status != http.StatusForbidden {
		t.Errorf("giving an entry away answered %d, want 403", answer.Status)
	}

	// Still Anna's, and still there.
	var still timesheetResponse
	anna.must(anna.api(http.MethodGet, path("/timesheets/", own.ID), nil), http.StatusOK).
		Data(t, &still)

	if still.UserID == bertID {
		t.Error("the entry now belongs to the colleague")
	}

	// And Bert sees nothing new.
	var bertsList struct {
		Items []timesheetResponse `json:"items"`
	}

	bert.must(bert.api(http.MethodGet, "/timesheets", nil), http.StatusOK).Data(t, &bertsList)

	if len(bertsList.Items) != 0 {
		t.Errorf("the colleague now has %d entry(s) they did not record", len(bertsList.Items))
	}
}

// Editing an entry onto a project it may not have is refused, exactly as booking
// onto it would be.
//
// Booking and transferring both check that the project is one this person may see
// and that it still accepts hours. Editing checked only that it existed, so the two
// rules could be walked around by making the same change through PUT: hours into a
// colleague's private category, which the API refuses even to admit exists, or onto
// a project that had been completed.
func TestEditingCannotMoveAnEntryWhereBookingCouldNot(t *testing.T) {
	_, anna, bert := twoColleagues(t)

	// Bert's own category, which Anna may not even know about.
	var bertsCategory projectResponse
	bert.must(bert.api(http.MethodPost, "/projects", map[string]any{
		"name": "Bert's own", "startDate": "2026-08-01", "private": true,
	}), http.StatusCreated, http.StatusOK).Data(t, &bertsCategory)

	// And a shared project that has been finished.
	var finished projectResponse
	anna.must(anna.api(http.MethodPost, "/projects", map[string]any{
		"name": "Finished", "startDate": "2026-08-01",
	}), http.StatusCreated, http.StatusOK).Data(t, &finished)

	anna.must(anna.api(http.MethodPut, path("/projects/", finished.ID),
		map[string]any{"status": "completed"}), http.StatusOK)

	own := entryOf(t, anna, map[string]any{"date": "2026-08-06", "durationHours": 2})

	// A private project is reported as absent rather than forbidden, which is what
	// keeps its existence private - the same answer booking gives.
	if got := anna.api(http.MethodPut, path("/timesheets/", own.ID),
		map[string]any{"projectId": bertsCategory.ID}).Status; got != http.StatusNotFound {
		t.Errorf("editing an entry onto a colleague's private category answered %d, "+
			"want 404", got)
	}

	if got := anna.api(http.MethodPut, path("/timesheets/", own.ID),
		map[string]any{"projectId": finished.ID}).Status; got != http.StatusConflict {
		t.Errorf("editing an entry onto a completed project answered %d, want 409", got)
	}

	// Neither attempt moved it.
	var still timesheetResponse
	anna.must(anna.api(http.MethodGet, path("/timesheets/", own.ID), nil), http.StatusOK).
		Data(t, &still)

	if still.ProjectID != nil {
		t.Errorf("the entry ended up on project %d", *still.ProjectID)
	}
}

// Editing everything else about an entry on a finished project still works.
//
// The rule is about moving hours onto a closed project, not about the entry that is
// already there: a typo in its description has to stay fixable, or the check has
// replaced one annoyance with another.
func TestAnEntryOnAFinishedProjectStaysEditable(t *testing.T) {
	_, anna, _ := twoColleagues(t)

	var project projectResponse
	anna.must(anna.api(http.MethodPost, "/projects", map[string]any{
		"name": "Winding down", "startDate": "2026-08-01",
	}), http.StatusCreated, http.StatusOK).Data(t, &project)

	own := entryOf(t, anna, map[string]any{
		"date": "2026-08-07", "durationHours": 2, "projectId": project.ID,
		"description": "typo hree",
	})

	anna.must(anna.api(http.MethodPut, path("/projects/", project.ID),
		map[string]any{"status": "completed"}), http.StatusOK)

	var fixed timesheetResponse
	anna.must(anna.api(http.MethodPut, path("/timesheets/", own.ID),
		map[string]any{"description": "typo here"}), http.StatusOK).Data(t, &fixed)

	if fixed.Description == nil || *fixed.Description != "typo here" {
		t.Errorf("the description was not corrected: %+v", fixed.Description)
	}
}
