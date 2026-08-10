//go:build integration

package integration

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/pkg/spreadsheet"
)

// Projects and people go in and out as a spreadsheet too, each from its own tab.
//
// What these tests are really about is the two ways this can go quietly wrong:
// "export" being read as an id by the router, which answered 400 the first time a
// pair of these routes was added, and an import that reaches further than the
// screen it sits on.

type sheetRow struct {
	Row     int      `json:"row"`
	Cells   []string `json:"cells"`
	Problem string   `json:"problem"`
}

type sheetResult struct {
	DryRun   bool       `json:"dryRun"`
	Columns  []string   `json:"columns"`
	Rows     []sheetRow `json:"rows"`
	Writable int        `json:"writable"`
	Rejected int        `json:"rejected"`
	Imported int        `json:"imported"`
}

func importSheet(
	t *testing.T,
	c *client,
	path string,
	book []byte,
	dryRun string,
) (sheetResult, response) {
	t.Helper()

	r := c.upload(path, "file", "sheet.xlsx", book, map[string]string{"dryRun": dryRun})

	var out sheetResult

	if r.Status == http.StatusOK || r.Status == http.StatusCreated {
		r.Data(t, &out)
	}

	return out, r
}

// accepted reports whether an upload was answered, which is 201 for a write and
// 200 or 201 for a preview depending on the route.
func accepted(status int) bool {
	return status == http.StatusOK || status == http.StatusCreated
}

// The export routes are reachable, which is not a given: they sit beside
// /projects/{id} and /users/{id} and the router matches the first pattern that
// fits, so "export" read as an id is the failure to watch for.
func TestTheExportRoutesAreNotReadAsIdentifiers(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	for _, path := range []string{"/projects/export", "/users/export"} {
		exported := admin.must(admin.api(http.MethodGet, path, nil), http.StatusOK)

		// An .xlsx is a zip. A 400 about a parameter nobody sent is what this
		// catches.
		if !bytes.HasPrefix(exported.Body, []byte("PK")) {
			t.Errorf("%s did not answer with a workbook: %.40q", path, exported.Body)
		}
	}
}

// A project sheet round-trips: exported, read back, imported again, and the second
// import changes nothing rather than creating everything twice.
func TestProjectsRoundTripThroughASpreadsheet(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	// As an employee, not as the administrator: projects belong to the people doing
	// the work, and the system administrator administers. The administrator cannot
	// create a shared project at all, which the last test here pins down.
	worker := a.signInAsUser(admin, "Mika", "mika@example.com")

	for _, project := range []map[string]any{
		{"name": "Roof", "startDate": "2026-08-01", "description": "tiles"},
		{"name": "Cellar", "startDate": "2026-08-02", "endDate": "2026-09-30"},
	} {
		worker.must(worker.api(http.MethodPost, "/projects", project),
			http.StatusCreated, http.StatusOK)
	}

	exported := worker.must(worker.api(http.MethodGet, "/projects/export?lang=de", nil),
		http.StatusOK)

	rows, problems, err := spreadsheet.ReadProjects(bytes.NewReader(exported.Body))
	if err != nil {
		t.Fatalf("reading back a German export: %v", err)
	}

	if len(problems) != 0 {
		t.Fatalf("a German export came back with problems: %v", problems)
	}

	if len(rows) != 2 {
		t.Fatalf("exported %d projects, want 2", len(rows))
	}

	// Importing what was exported is a no-op in effect: every name is already
	// there, so each row is an update to the values it already holds.
	result, r := importSheet(t, worker, "/projects/import", exported.Body, "false")
	if !accepted(r.Status) {
		t.Fatalf("importing the export back: status %d, body %.200q", r.Status, r.Body)
	}

	if result.Rejected != 0 {
		t.Errorf("the export was refused on re-import: %+v", result.Rows)
	}

	if result.Imported != 2 {
		t.Errorf("wrote %d rows, want 2", result.Imported)
	}

	listed := worker.must(worker.api(http.MethodGet, "/projects", nil), http.StatusOK)

	var projects struct {
		Items []projectResponse `json:"items"`
	}

	listed.Data(t, &projects)

	// Two, not four: matched by name rather than added again.
	if len(projects.Items) != 2 {
		t.Errorf("there are %d projects after re-importing the export, want 2; names: %v",
			len(projects.Items), projects.Items)
	}
}

// An archived project survives being exported and imported again.
//
// It did not. Archiving is only allowed from "completed", and the rule reads the
// status the project has now - so a row saying "archived" about a project that is
// already archived was refused, for a change nobody had asked for. The status is
// only sent where it differs now, which is both correct and sidesteps the rule.
func TestAnArchivedProjectCanBeImportedBack(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")
	worker := a.signInAsUser(admin, "Mika", "mika@example.com")

	var project projectResponse
	worker.must(worker.api(http.MethodPost, "/projects", map[string]any{
		"name": "Finished roof", "startDate": "2026-08-01",
	}), http.StatusCreated, http.StatusOK).Data(t, &project)

	// Completed first, because that is the only status archiving is allowed from.
	for _, status := range []string{"completed", "archived"} {
		worker.must(worker.api(http.MethodPut, fmt.Sprintf("/projects/%d", project.ID),
			map[string]any{"status": status}), http.StatusOK)
	}

	exported := worker.must(worker.api(http.MethodGet, "/projects/export", nil), http.StatusOK)

	result, r := importSheet(t, worker, "/projects/import", exported.Body, "false")
	if !accepted(r.Status) {
		t.Fatalf("importing an export holding an archived project: status %d, body %.300q",
			r.Status, r.Body)
	}

	if result.Rejected != 0 {
		t.Errorf("the export was refused on re-import: %+v", result.Rows)
	}

	// And it is still archived rather than having been moved anywhere.
	listed := worker.must(worker.api(http.MethodGet, "/projects", nil), http.StatusOK)

	var projects struct {
		Items []projectResponse `json:"items"`
	}

	listed.Data(t, &projects)

	for _, item := range projects.Items {
		if item.ID == project.ID && item.Status != "archived" {
			t.Errorf("the project is %q after a round trip, want archived", item.Status)
		}
	}
}

// Asking for archiving where the rule forbids it is refused in the preview, not at
// the write.
//
// A preview that promised a row the write would refuse would be worse than no
// preview: nothing is written when any row is refused, so the whole file would fail
// after being shown as fine.
func TestArchivingAProjectThatIsNotCompletedIsRefusedInThePreview(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")
	worker := a.signInAsUser(admin, "Mika", "mika@example.com")

	worker.must(worker.api(http.MethodPost, "/projects", map[string]any{
		"name": "Busy roof", "startDate": "2026-08-01",
	}), http.StatusCreated, http.StatusOK)

	// The same project, asked to jump straight to archived.
	book, err := spreadsheet.WriteProjects("", []spreadsheet.ProjectRow{
		{Name: "Busy roof", StartDate: mustDay(t, "2026-08-01"), Status: "archived"},
	})
	if err != nil {
		t.Fatalf("building the workbook: %v", err)
	}

	preview, r := importSheet(t, worker, "/projects/import", book, "true")
	if !accepted(r.Status) {
		t.Fatalf("previewing: status %d, body %.200q", r.Status, r.Body)
	}

	if preview.Rejected != 1 || preview.Writable != 0 {
		t.Fatalf("preview says %d writable and %d refused, want 0 and 1: %+v",
			preview.Writable, preview.Rejected, preview.Rows)
	}

	if len(preview.Rows) != 1 || !strings.Contains(preview.Rows[0].Problem, "completed") {
		t.Errorf("the reason given does not mention the rule: %+v", preview.Rows)
	}
}

// The headings are translated and the values with them, and the preview says the
// same words as the file.
func TestTheProjectPreviewSpeaksTheLanguageAsked(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")
	worker := a.signInAsUser(admin, "Mika", "mika@example.com")

	book, err := spreadsheet.WriteProjects("de", []spreadsheet.ProjectRow{
		{Name: "Dach", StartDate: mustDay(t, "2026-08-01"), Status: "archived"},
	})
	if err != nil {
		t.Fatalf("building a German workbook: %v", err)
	}

	result, r := importSheet(t, worker, "/projects/import?lang=de", book, "true")
	if !accepted(r.Status) {
		t.Fatalf("previewing a German workbook: status %d, body %.200q", r.Status, r.Body)
	}

	if len(result.Columns) == 0 || result.Columns[0] != "Name" {
		t.Fatalf("the preview has no German columns: %v", result.Columns)
	}

	if result.Columns[2] != "Beginn" {
		t.Errorf("column 3 is %q, want %q", result.Columns[2], "Beginn")
	}

	if result.Rejected != 0 {
		t.Fatalf("a German workbook was refused: %+v", result.Rows)
	}

	// The status went out as "archiviert", was understood as "archived", and comes
	// back into the preview as "archiviert" - the reader compares the preview with
	// their file, so the two have to say the same word.
	if len(result.Rows) != 1 || len(result.Rows[0].Cells) < 5 {
		t.Fatalf("unexpected preview shape: %+v", result.Rows)
	}

	if got := result.Rows[0].Cells[4]; got != "archiviert" {
		t.Errorf("the preview shows the status as %q, want %q", got, "archiviert")
	}
}

// A sheet of the wrong kind is refused rather than half understood.
func TestASheetOfTheWrongKindIsRefusedByTheAPI(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	entries := workbookOf(t, spreadsheet.Row{
		Date: mustDay(t, "2026-08-03"), User: "somebody", Hours: 2,
	})

	_, r := importSheet(t, admin, "/users/import", entries, "true")

	if accepted(r.Status) {
		t.Errorf("a sheet of time entries was accepted by the people importer: %.300q", r.Body)
	}
}

// An import of people changes accounts and does not invent them.
func TestImportingPeopleChangesThemAndCreatesNothing(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")
	a.signInAsUser(admin, "Mika", "mika@example.com")

	book, err := spreadsheet.WriteUsers("", []spreadsheet.UserRow{
		{Name: "Mika Renamed", Email: "mika@example.com", Role: "employee"},
		{Name: "Nobody", Email: "nobody@example.com", Role: "employee"},
	})
	if err != nil {
		t.Fatalf("building the workbook: %v", err)
	}

	preview, r := importSheet(t, admin, "/users/import", book, "true")
	if !accepted(r.Status) {
		t.Fatalf("previewing: status %d, body %.200q", r.Status, r.Body)
	}

	if preview.Writable != 1 || preview.Rejected != 1 {
		t.Fatalf("preview says %d writable and %d refused, want 1 and 1: %+v",
			preview.Writable, preview.Rejected, preview.Rows)
	}

	// And with a refused row in it, nothing is written at all.
	written, r := importSheet(t, admin, "/users/import", book, "false")
	if accepted(r.Status) && written.Imported > 0 {
		t.Fatalf("a file with a refused row wrote %d rows", written.Imported)
	}

	// Mika is untouched, which is the point of refusing the whole file rather than
	// the one row.
	listed := admin.must(admin.api(http.MethodGet, "/users", nil), http.StatusOK)

	var people struct {
		Items []struct {
			Email string `json:"email"`
			Name  string `json:"name"`
		} `json:"items"`
	}

	listed.Data(t, &people)

	for _, person := range people.Items {
		if person.Email == "mika@example.com" && person.Name != "Mika" {
			t.Errorf("a file with a refused row in it still renamed an account to %q",
				person.Name)
		}
	}
}

// The sheet carries nothing the administrator may not set.
//
// It used to hold the daily target, the ceiling and the time zone, and the import
// wrote all three - which made a spreadsheet the widest way into figures that belong
// to the person they are about. Removing the write without removing the columns would
// have been worse than leaving it: somebody edits forty targets, is told forty rows
// were written, and nothing happened.
func TestThePeopleSheetCarriesNoWorkingTimes(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")
	a.signInAsUser(admin, "Mika", "mika@example.com")

	exported := admin.must(admin.api(http.MethodGet, "/users/export", nil), http.StatusOK)

	preview, r := importSheet(t, admin, "/users/import", exported.Body, "true")
	if !accepted(r.Status) {
		t.Fatalf("previewing the export: status %d, body %.200q", r.Status, r.Body)
	}

	for _, gone := range []string{"Daily target", "Daily maximum", "Time zone"} {
		for _, column := range preview.Columns {
			if column == gone {
				t.Errorf("the people sheet still has a %q column, which the import "+
					"cannot write - it would be silently ignored", gone)
			}
		}
	}

	// What it does carry, so this cannot pass by the sheet being empty.
	if len(preview.Columns) == 0 {
		t.Fatal("the people sheet has no columns at all")
	}

	want := map[string]bool{"Name": true, "Email": true, "Role": true, "Directory": true}

	for _, column := range preview.Columns {
		if !want[column] {
			t.Errorf("unexpected column %q in the people sheet", column)
		}
	}
}

// The two time-zone cases that were here are gone with the column they tested. A
// zone decides which calendar day a booking falls on, so it is a time figure, and
// everything to do with time belongs to the person it is about - who sets it under My
// account. TestThePeopleSheetCarriesNoWorkingTimes is what replaced them: it checks
// the column is absent rather than that a value in it is validated.

// The system administrator can keep private categories and cannot create shared
// projects, by import as by any other route.
//
// That separation is deliberate: everybody manages their own projects, and the
// system administrator administers the installation. An import that ignored it
// would be a way round it - which is exactly what a bulk route is at risk of
// being, since it is the one nobody looks at row by row.
func TestAnImportCannotReachPastTheScreenItSitsOn(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	book, err := spreadsheet.WriteProjects("", []spreadsheet.ProjectRow{
		{Name: "Everybody's roof", StartDate: mustDay(t, "2026-08-01"), Status: "active"},
		{Name: "My own admin", StartDate: mustDay(t, "2026-08-01"), Status: "active",
			Category: true},
	})
	if err != nil {
		t.Fatalf("building the workbook: %v", err)
	}

	preview, r := importSheet(t, admin, "/projects/import", book, "true")
	if !accepted(r.Status) {
		t.Fatalf("previewing as the administrator: status %d, body %.200q", r.Status, r.Body)
	}

	// The category is theirs to keep; the shared project is not theirs to create.
	if preview.Writable != 1 || preview.Rejected != 1 {
		t.Errorf("preview says %d writable and %d refused, want 1 and 1: %+v",
			preview.Writable, preview.Rejected, preview.Rows)
	}
}
