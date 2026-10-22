//go:build integration

package integration

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// Everything held about an account is handed over with it, a part per table, on
// every database - and none of the secrets: not the personal token or the
// calendar address the account was just shown once, and no column that holds a
// hash, a seed or a key. A part with nothing in it is there as an empty list.
func TestAnAccountIsHandedOverWholeAndWithoutItsSecrets(t *testing.T) {
	t.Parallel()

	_, _, worker := startWithWorker(t)

	var project struct {
		ID uint `json:"id"`
	}

	worker.must(worker.api(http.MethodPost, "/projects", map[string]any{
		"name": "Portal", "startDate": "2026-08-01", "budgetHours": 10,
	}), http.StatusCreated, http.StatusOK).Data(t, &project)

	var entry struct {
		ID uint `json:"id"`
	}

	worker.must(worker.api(http.MethodPost, "/timesheets", map[string]any{
		"date": "2026-08-03", "durationHours": 4, "projectId": project.ID, "description": "first",
	}), http.StatusCreated, http.StatusOK).Data(t, &entry)

	worker.must(worker.api(http.MethodPut, path("/timesheets/", entry.ID), map[string]any{
		"date": "2026-08-03", "durationHours": 5, "projectId": project.ID, "description": "corrected",
	}), http.StatusOK)

	for _, call := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/me/templates", map[string]any{"projectId": project.ID, "durationHours": 0.25, "description": "Daily"}},
		{http.MethodPost, "/me/absences", map[string]any{"from": "2026-08-10", "to": "2026-08-10", "kind": "vacation"}},
		{http.MethodPut, "/me/vacation", map[string]any{"fromYear": 2026, "days": 28}},
		{http.MethodPost, "/me/schedules", map[string]any{"validFrom": "2026-08-03", "hours": []float64{8, 8, 8, 8, 6, 0, 0}}},
		{http.MethodPut, "/me/workdays/2026-08-03", map[string]any{"start": "08:00", "end": "16:30", "breakMinutes": 30}},
		{http.MethodPost, "/me/timer", map[string]any{"description": "running"}},
	} {
		worker.must(worker.api(call.method, call.path, call.body), http.StatusOK, http.StatusCreated)
	}

	var token struct {
		Secret *string `json:"secret"`
	}

	worker.must(worker.api(http.MethodPost, "/me/tokens", map[string]any{"name": "ci", "expiresInDays": 30}),
		http.StatusCreated, http.StatusOK).Data(t, &token)

	var feed struct {
		Path *string `json:"path"`
	}

	worker.must(worker.api(http.MethodPost, "/me/calendar-feed", nil),
		http.StatusCreated, http.StatusOK).Data(t, &feed)

	if token.Secret == nil || feed.Path == nil {
		t.Fatalf("the token or the calendar address was not shown once: %v %v", token.Secret, feed.Path)
	}

	exported := worker.api(http.MethodGet, "/me/export", nil)
	if exported.Status != http.StatusOK {
		t.Fatalf("the export answered %d: %s", exported.Status, exported.Body)
	}

	archive, err := zip.NewReader(bytes.NewReader(exported.Body), int64(len(exported.Body)))
	if err != nil {
		t.Fatalf("the export is not a ZIP: %v", err)
	}

	parts := map[string][]map[string]any{}
	whole := ""

	for _, file := range archive.File {
		opened, err := file.Open()
		if err != nil {
			t.Fatalf("open %s: %v", file.Name, err)
		}

		content, _ := io.ReadAll(opened)
		_ = opened.Close()
		whole += string(content)

		if file.Name == "about.json" {
			continue
		}

		var rows []map[string]any
		if err := json.Unmarshal(content, &rows); err != nil {
			t.Fatalf("%s is not a list of rows: %v", file.Name, err)
		}

		parts[strings.TrimSuffix(file.Name, ".json")] = rows
	}

	for _, part := range []string{"account", "stopwatch", "entry-history", "time-entries", "projects",
		"api-tokens", "sessions", "planned-weeks", "entry-templates", "absences", "leave-entitlements",
		"calendar-subscription", "working-times"} {
		if len(parts[part]) == 0 {
			t.Errorf("%s holds nothing, though the account has some", part)
		}
	}

	for _, part := range []string{"passkeys", "openid-links"} {
		if rows, there := parts[part]; !there || len(rows) != 0 {
			t.Errorf("%s reads %v, want an empty list", part, rows)
		}
	}

	if history := parts["entry-history"]; len(history) == 1 && history[0]["description"] != "first" {
		t.Errorf("the history holds %v, want the version the correction replaced", history[0])
	}

	key := (*feed.Path)[strings.LastIndex(*feed.Path, "/")+1:]

	for _, secret := range []string{*token.Secret, strings.TrimSuffix(key, ".ics"),
		"password_hash", "totp_secret", "token_hash", "key_hash", "public_key", "credential_id"} {
		if strings.Contains(whole, secret) {
			t.Errorf("the export holds %q", secret)
		}
	}
}
