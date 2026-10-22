package service_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"slices"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
)

// partsOf stands in for the database: it answers with the parts it was given, for
// the account it was asked about.
type partsOf map[uint][]repository.ExportedPart

func (p partsOf) ExportUser(_ context.Context, userID uint) ([]repository.ExportedPart, error) {
	return p[userID], nil
}

// An export is a ZIP with a JSON file a part, every row in it as it was read, and
// a note saying what it is and what was left out - a part with no rows included,
// because "nothing" is an answer to "what do you keep".
func TestAnExportIsAFileAPartAndANoteOnWhatIsLeftOut(t *testing.T) {
	export := service.NewAccountExportService(partsOf{7: {
		{Name: "account", Rows: []map[string]any{{"id": 7, "email": "wera@example.com"}}},
		{Name: "time-entries", Rows: []map[string]any{{"date": "2026-10-20", "duration_hours": 7.5}}},
		{Name: "passkeys", Rows: []map[string]any{}},
	}})

	archive, err := export.Export(context.Background(), 7)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	files := map[string][]byte{}

	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatalf("what came back is not a ZIP: %v", err)
	}

	for _, file := range reader.File {
		opened, err := file.Open()
		if err != nil {
			t.Fatalf("open %s: %v", file.Name, err)
		}

		files[file.Name], _ = io.ReadAll(opened)
		_ = opened.Close()
	}

	var names []string
	for name := range files {
		names = append(names, name)
	}

	slices.Sort(names)

	if want := []string{"about.json", "account.json", "passkeys.json", "time-entries.json"}; !slices.Equal(names, want) {
		t.Fatalf("the ZIP holds %v, want %v", names, want)
	}

	var entries []map[string]any
	if err := json.Unmarshal(files["time-entries.json"], &entries); err != nil || len(entries) != 1 ||
		entries[0]["duration_hours"] != 7.5 {
		t.Errorf("time-entries.json reads %s (%v)", files["time-entries.json"], err)
	}

	var none []map[string]any
	if err := json.Unmarshal(files["passkeys.json"], &none); err != nil || none == nil || len(none) != 0 {
		t.Errorf("an empty part reads %s, want an empty list", files["passkeys.json"])
	}

	var about struct {
		Parts   []string `json:"parts"`
		LeftOut []string `json:"leftOut"`
	}
	if err := json.Unmarshal(files["about.json"], &about); err != nil || len(about.Parts) != 3 || len(about.LeftOut) == 0 {
		t.Errorf("about.json reads %s", files["about.json"])
	}
}
