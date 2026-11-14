//go:build integration

package integration

import (
	"net/http"
	"testing"
)

type adminChanges struct {
	Entries []struct {
		Action string `json:"action"`
		Actor  struct {
			ID   uint   `json:"id"`
			Name string `json:"name"`
		} `json:"actor"`
		Target *struct {
			ID   uint   `json:"id"`
			Name string `json:"name"`
		} `json:"target"`
		Detail string `json:"detail"`
	} `json:"entries"`
	More bool `json:"more"`
}

// What an administrator changes is recorded with who changed it, through the
// whole stack on every database: an account made, given another role and erased,
// newest first, the erased one kept under its id and no name. The log is the
// administrators' to read, and nobody else's.
func TestTheAdministrationLogRecordsWhoChangedWhat(t *testing.T) {
	t.Parallel()

	_, admin, worker := startWithWorker(t)

	var made struct {
		ID uint `json:"id"`
	}

	admin.must(admin.api(http.MethodPost, "/users", map[string]any{
		"name": "Bea", "email": "bea@example.com", "role": "user", "password": "bea-password-1",
	}), http.StatusCreated, http.StatusOK).Data(t, &made)

	admin.must(admin.api(http.MethodPut, path("/users/", made.ID, "/role"), map[string]any{"role": "user-admin"}),
		http.StatusOK)
	admin.must(admin.api(http.MethodDelete, path("/users/", made.ID), nil), http.StatusOK, http.StatusNoContent)

	var log adminChanges

	admin.must(admin.api(http.MethodGet, "/admin/changes", nil), http.StatusOK).Data(t, &log)

	if len(log.Entries) < 3 {
		t.Fatalf("the log holds %+v, want the three changes", log.Entries)
	}

	deleted, role, created := log.Entries[0], log.Entries[1], log.Entries[2]

	for _, entry := range []struct {
		action string
		got    string
	}{{"user.deleted", deleted.Action}, {"user.role", role.Action}, {"user.created", created.Action}} {
		if entry.got != entry.action {
			t.Errorf("an entry is %q where %q was expected: %+v", entry.got, entry.action, log.Entries[:3])
		}
	}

	if created.Actor.Name == "" || created.Target == nil || created.Target.ID != made.ID || created.Target.Name != "" {
		t.Errorf("the creation reads %+v, want the administrator named and the erased account kept by id alone", created)
	}

	if role.Detail != "user-admin" {
		t.Errorf("the role change keeps %q, want user-admin", role.Detail)
	}

	if refused := worker.api(http.MethodGet, "/admin/changes", nil); refused.Status != http.StatusForbidden {
		t.Errorf("an ordinary account asking for the log was answered %d, want 403", refused.Status)
	}
}

// Checking a file before importing it changes nothing, and the log says so: the
// import is recorded once, and the check before it not at all. The check is the
// same request as the import with dryRun set, which is what let the route's
// entry in the log's table take it for an import of its own.
func TestCheckingAnImportIsNotRecordedAsOne(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	exported := admin.must(admin.api(http.MethodGet, "/roles/export", nil), http.StatusOK)

	imports := func() int {
		var log adminChanges

		admin.must(admin.api(http.MethodGet, "/admin/changes", nil), http.StatusOK).Data(t, &log)

		counted := 0

		for _, entry := range log.Entries {
			if entry.Action == "roles.imported" {
				counted++
			}
		}

		return counted
	}

	if _, r := importSheet(t, admin, "/roles/import", exported.Body, "true"); !accepted(r.Status) {
		t.Fatalf("checking the file: status %d, body %.300q", r.Status, r.Body)
	}

	if got := imports(); got != 0 {
		t.Errorf("after checking a file the log holds %d imports, want none", got)
	}

	if _, r := importSheet(t, admin, "/roles/import", exported.Body, "false"); !accepted(r.Status) {
		t.Fatalf("importing the file: status %d, body %.300q", r.Status, r.Body)
	}

	if got := imports(); got != 1 {
		t.Errorf("after checking a file and importing it the log holds %d imports, want one", got)
	}
}
