//go:build integration

package integration

import (
	"net/http"
	"testing"
)

type templateOnTheWire struct {
	ID            uint     `json:"id"`
	ProjectID     *uint    `json:"projectId"`
	DurationHours *float64 `json:"durationHours"`
	Description   *string  `json:"description"`
}

func templatesOf(t *testing.T, c *client) []templateOnTheWire {
	t.Helper()

	var list listOf[templateOnTheWire]

	c.must(c.api(http.MethodGet, "/me/templates", nil), http.StatusOK).Data(t, &list)

	return list.Items
}

// A template is kept once however often it is saved, read back as it was saved,
// and deleted.
func TestATemplateIsKeptReadAndDeleted(t *testing.T) {
	t.Parallel()

	_, _, worker := startWithWorker(t)

	var portal projectResponse

	worker.must(worker.api(http.MethodPost, "/projects", map[string]any{
		"name": "Portal", "startDate": "2026-08-01",
	}), http.StatusCreated, http.StatusOK).Data(t, &portal)

	daily := map[string]any{"projectId": portal.ID, "durationHours": 0.25, "description": "Daily"}

	var first, again templateOnTheWire

	worker.must(worker.api(http.MethodPost, "/me/templates", daily),
		http.StatusCreated, http.StatusOK).Data(t, &first)
	worker.must(worker.api(http.MethodPost, "/me/templates", daily),
		http.StatusCreated, http.StatusOK).Data(t, &again)

	if again.ID != first.ID {
		t.Errorf("the same template saved twice became %d and %d, want one", first.ID, again.ID)
	}

	kept := templatesOf(t, worker)

	if len(kept) != 1 || kept[0].ProjectID == nil || *kept[0].ProjectID != portal.ID ||
		kept[0].DurationHours == nil || *kept[0].DurationHours != 0.25 ||
		kept[0].Description == nil || *kept[0].Description != "Daily" {
		t.Fatalf("the templates read %+v, want the one saved", kept)
	}

	worker.must(worker.api(http.MethodDelete, path("/me/templates/", first.ID), nil), http.StatusOK, http.StatusNoContent)

	if left := templatesOf(t, worker); len(left) != 0 {
		t.Errorf("%d template(s) left after deleting the one", len(left))
	}
}

// A colleague's template is not there to read or to delete.
func TestAColleaguesTemplateIsNotThere(t *testing.T) {
	t.Parallel()

	_, anna, bert := twoColleagues(t)

	var annas templateOnTheWire

	anna.must(anna.api(http.MethodPost, "/me/templates", map[string]any{"description": "Anna's daily"}),
		http.StatusCreated, http.StatusOK).Data(t, &annas)

	if seen := templatesOf(t, bert); len(seen) != 0 {
		t.Errorf("a colleague's list shows %+v, want nothing of Anna's", seen)
	}

	if refused := bert.api(http.MethodDelete, path("/me/templates/", annas.ID), nil); refused.Status != http.StatusNotFound {
		t.Errorf("deleting a colleague's template answered %d, want 404", refused.Status)
	}

	if kept := templatesOf(t, anna); len(kept) != 1 {
		t.Errorf("Anna has %d template(s) after a colleague tried to delete hers, want 1", len(kept))
	}
}

// An account's templates go with it, on every database: PostgreSQL refuses the
// deletion outright if one is left pointing at the account.
func TestDeletingAnAccountTakesItsTemplatesWithIt(t *testing.T) {
	t.Parallel()

	a, admin, worker := startWithWorker(t)

	var me struct {
		User userResponse `json:"user"`
	}

	worker.must(worker.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &me)

	worker.must(worker.api(http.MethodPost, "/me/templates", map[string]any{"description": "Daily"}),
		http.StatusCreated, http.StatusOK)

	admin.must(admin.api(http.MethodDelete, path("/users/", me.User.ID), nil), http.StatusOK, http.StatusNoContent)

	if left := a.Rows(t, "entry_templates", "user_id = ?", me.User.ID); left != 0 {
		t.Errorf("%d template(s) outlived the account they belonged to", left)
	}
}
