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

	// Why it was refused, as something other than an English reader can act on.
	ProblemCode   string `json:"problemCode"`
	ProblemValues []any  `json:"problemValues"`
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
//
// One caller per route, because the sheets belong to the two jobs: a project
// sheet is somebody's own work, so it is asked for by somebody who works here, and
// the accounts and roles are the installation's, so they are asked for by the
// administrator. Neither account can stand in for the other - the administrator
// holds nothing about projects, and an ordinary account holds nothing about
// accounts - so a single caller could only ever prove half of this.
func TestTheExportRoutesAreNotReadAsIdentifiers(t *testing.T) {
	_, admin, worker := startWithWorker(t)

	for _, sheet := range []struct {
		route  string
		caller *client
	}{
		{"/projects/export", worker},
		{"/users/export", admin},
		{"/roles/export", admin},
	} {
		exported := sheet.caller.must(sheet.caller.api(http.MethodGet, sheet.route, nil),
			http.StatusOK)

		// An .xlsx is a zip. A 400 about a parameter nobody sent is what this
		// catches.
		if !bytes.HasPrefix(exported.Body, []byte("PK")) {
			t.Errorf("%s did not answer with a workbook: %.40q", sheet.route, exported.Body)
		}
	}
}

// A project sheet round-trips: exported, read back, imported again, and the second
// import changes nothing rather than creating everything twice.
func TestProjectsRoundTripThroughASpreadsheet(t *testing.T) {
	_, _, worker := startWithWorker(t)

	// The work is done by somebody who works here; the administrator only creates
	// that account. It holds no project rights of its own at all.
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
	_, _, worker := startWithWorker(t)

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
	_, _, worker := startWithWorker(t)

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
	_, _, worker := startWithWorker(t)

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
		{Name: "Mika Renamed", Email: "mika@example.com", Role: "user"},
		{Name: "Nobody", Email: "nobody@example.com", Role: "user"},
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

// The roles sheet, which is the one that carries a column per right.
//
// It was left out of this file for a while on the grounds that a role is a list of
// permissions and a spreadsheet cannot hold one honestly. The objection was to a
// cell reading "projects:read,projects:write,..." - a typo in which drops a right
// without failing - and the sheet answers it by giving each right a column of its
// own holding yes or no. That makes an unrecognised right a heading rather than a
// word in a list, which is a thing the reader can refuse by name; the cases below
// are what hold it to that.

// countRoles answers how many roles exist, which is how an import that matched
// rows is told from one that added them all again.
func countRoles(t *testing.T, c *client) int {
	t.Helper()

	var roles struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
	}

	c.must(c.api(http.MethodGet, "/roles", nil), http.StatusOK).Data(t, &roles)

	return len(roles.Items)
}

// permissionsOffered reads the rights off the sheet's own headings.
//
// Rather than importing the list the application builds them from: what is being
// checked is that the file and the application agree about what a right is called,
// and asking one source twice would check nothing.
func permissionsOffered(t *testing.T, columns []string) []string {
	t.Helper()

	// Name, Description and System come first and are translated; the permission
	// columns follow and are not, because a permission is an identifier.
	const fixed = 3

	if len(columns) <= fixed {
		t.Fatalf("the roles sheet has no permission columns at all: %v", columns)
	}

	return columns[fixed:]
}

// A roles sheet round-trips: exported, imported again, and the second import
// matches every role by name instead of creating a second set of them.
func TestRolesRoundTripThroughASpreadsheet(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	before := countRoles(t, admin)

	exported := admin.must(admin.api(http.MethodGet, "/roles/export", nil), http.StatusOK)

	written, r := importSheet(t, admin, "/roles/import", exported.Body, "false")
	if !accepted(r.Status) {
		t.Fatalf("importing the export back: status %d, body %.300q", r.Status, r.Body)
	}

	// Nothing refused, including the system role: its rights arrive exactly as they
	// left, and unchanged rights are not a change to a system role.
	if written.Rejected != 0 {
		t.Errorf("the export was refused on re-import: %+v", written.Rows)
	}

	if after := countRoles(t, admin); after != before {
		t.Errorf("there are %d roles after re-importing the export, want %d", after, before)
	}
}

// A file may name a right this application does not enforce, and it is refused by
// that name rather than quietly ignored.
//
// This is the whole reason the sheet has a column per right. A heading is
// something the reader can look up and complain about; a misspelled word inside a
// list of rights is a right nobody granted and nobody was told about.
func TestARightThisApplicationDoesNotEnforceIsRefusedByName(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	exported := admin.must(admin.api(http.MethodGet, "/roles/export", nil), http.StatusOK)

	preview, r := importSheet(t, admin, "/roles/import", exported.Body, "true")
	if !accepted(r.Status) {
		t.Fatalf("previewing the export: status %d, body %.300q", r.Status, r.Body)
	}

	const invented = "projects:teleport"

	book, err := spreadsheet.WriteRoles("",
		append(permissionsOffered(t, preview.Columns), invented),
		[]spreadsheet.RoleRow{{Name: "helper", Granted: []string{invented}}})
	if err != nil {
		t.Fatalf("building the workbook: %v", err)
	}

	_, r = importSheet(t, admin, "/roles/import", book, "true")
	if accepted(r.Status) {
		t.Fatalf("a sheet naming a right nobody enforces was accepted: %.300q", r.Body)
	}

	// And the refusal says which column, because "the file is wrong" leaves
	// somebody comparing forty headings by eye.
	if !strings.Contains(string(r.Body), invented) {
		t.Errorf("the refusal does not name the column: %.300q", r.Body)
	}
}

// A role that does not exist yet is created from a row, which is what makes this
// sheet different from the accounts one: everything a role is fits in a row.
func TestARoleCanBeCreatedFromASheet(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	exported := admin.must(admin.api(http.MethodGet, "/roles/export", nil), http.StatusOK)

	preview, r := importSheet(t, admin, "/roles/import", exported.Body, "true")
	if !accepted(r.Status) {
		t.Fatalf("previewing the export: status %d, body %.300q", r.Status, r.Body)
	}

	// One right, granted by ticking its column - the sheet's answer to a list in a
	// cell.
	granted := permissionsOffered(t, preview.Columns)[:1]

	book, err := spreadsheet.WriteRoles("", permissionsOffered(t, preview.Columns),
		[]spreadsheet.RoleRow{{
			Name: "auditor", Description: "reads and nothing else", Granted: granted,
		}})
	if err != nil {
		t.Fatalf("building the workbook: %v", err)
	}

	written, r := importSheet(t, admin, "/roles/import", book, "false")
	if !accepted(r.Status) {
		t.Fatalf("importing a new role: status %d, body %.300q", r.Status, r.Body)
	}

	if written.Imported != 1 {
		t.Fatalf("wrote %d rows, want 1: %+v", written.Imported, written.Rows)
	}

	var roles struct {
		Items []struct {
			Name        string   `json:"name"`
			Permissions []string `json:"permissions"`
		} `json:"items"`
	}

	admin.must(admin.api(http.MethodGet, "/roles", nil), http.StatusOK).Data(t, &roles)

	for _, role := range roles.Items {
		if role.Name != "auditor" {
			continue
		}

		if len(role.Permissions) != 1 || role.Permissions[0] != granted[0] {
			t.Errorf("the created role holds %v, want %v", role.Permissions, granted)
		}

		return
	}

	t.Errorf("the imported role is not there: %+v", roles.Items)
}

// A system role cannot be stripped of its rights by editing a cell.
//
// The role editor refuses this, and a bulk route that did not would make the
// spreadsheet the widest way into the one thing that keeps an installation
// administrable - on the route nobody reads row by row.
func TestASystemRoleCannotBeStrippedByASheet(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	exported := admin.must(admin.api(http.MethodGet, "/roles/export", nil), http.StatusOK)

	preview, r := importSheet(t, admin, "/roles/import", exported.Body, "true")
	if !accepted(r.Status) {
		t.Fatalf("previewing the export: status %d, body %.300q", r.Status, r.Body)
	}

	permissions := permissionsOffered(t, preview.Columns)

	// The built-in administrator's own role, granted one right instead of the ones
	// it needs.
	book, err := spreadsheet.WriteRoles("", permissions, []spreadsheet.RoleRow{{
		Name: "admin", Description: "narrowed", Granted: permissions[:1],
	}})
	if err != nil {
		t.Fatalf("building the workbook: %v", err)
	}

	narrowed, r := importSheet(t, admin, "/roles/import", book, "true")
	if !accepted(r.Status) {
		t.Fatalf("previewing: status %d, body %.300q", r.Status, r.Body)
	}

	if narrowed.Rejected != 1 || narrowed.Writable != 0 {
		t.Fatalf("the preview says %d refused and %d writable, want 1 and 0: %+v",
			narrowed.Rejected, narrowed.Writable, narrowed.Rows)
	}

	// And the write does nothing, rather than the preview being the only guard.
	applied, r := importSheet(t, admin, "/roles/import", book, "false")
	if accepted(r.Status) && applied.Imported > 0 {
		t.Fatalf("a file narrowing a system role wrote %d rows", applied.Imported)
	}

	// The administrator can still administer, which is what the rule is for.
	admin.must(admin.api(http.MethodGet, "/roles", nil), http.StatusOK)
	admin.must(admin.api(http.MethodGet, "/users", nil), http.StatusOK)
}

// A refused row says why in a form the reader's own language can be built from.
//
// The preview translates its headings and its cells - a German export comes back
// with "archiviert" in the status column - and the column saying why a row cannot
// be written was English prose beside all of it. It travels as a code and the
// values the sentence interpolated now, the same way a refused request does.
func TestARefusedRowNamesItsReasonAndNotOnlyInEnglish(t *testing.T) {
	_, _, worker := startWithWorker(t)

	book, err := spreadsheet.WriteProjects("", []spreadsheet.ProjectRow{
		{Name: "Roof", StartDate: mustDay(t, "2026-08-01"), Status: "half-done"},
	})
	if err != nil {
		t.Fatalf("building the workbook: %v", err)
	}

	preview, r := importSheet(t, worker, "/projects/import", book, "true")
	if !accepted(r.Status) {
		t.Fatalf("previewing: status %d, body %.300q", r.Status, r.Body)
	}

	if len(preview.Rows) != 1 {
		t.Fatalf("the preview has %d rows, want 1: %+v", len(preview.Rows), preview.Rows)
	}

	row := preview.Rows[0]

	if row.Problem == "" {
		t.Fatal("a row with a status nobody recognises was accepted")
	}

	if row.ProblemCode != "notAStatus" {
		t.Errorf("the refusal is coded %q, want %q - without a code the interface can "+
			"only show the English sentence", row.ProblemCode, "notAStatus")
	}

	// And the value the sentence is about, so a translation can put it back in its
	// own word order rather than being a sentence with a hole in it.
	if len(row.ProblemValues) == 0 || row.ProblemValues[0] != "half-done" {
		t.Errorf("the refusal carries %v, want the offending status first",
			row.ProblemValues)
	}
}

// The same for a row the reader itself could not understand, which is a different
// path: those complaints are made before any of the application's rules are.
func TestAnUnreadableRowNamesItsReason(t *testing.T) {
	_, _, worker := startWithWorker(t)

	book, err := spreadsheet.WriteProjects("", []spreadsheet.ProjectRow{
		{Name: "", StartDate: mustDay(t, "2026-08-01"), Status: "active"},
	})
	if err != nil {
		t.Fatalf("building the workbook: %v", err)
	}

	preview, r := importSheet(t, worker, "/projects/import", book, "true")
	if !accepted(r.Status) {
		t.Fatalf("previewing: status %d, body %.300q", r.Status, r.Body)
	}

	if len(preview.Rows) != 1 || preview.Rows[0].ProblemCode != "nameMissing" {
		t.Errorf("a nameless row is refused as %+v, want the code %q",
			preview.Rows, "nameMissing")
	}
}

// An import creates projects of the importer's own, and nobody else sees them.
//
// This case used to be about two kinds of project: it checked that somebody who could
// only keep private categories was refused a row asking for a shared project. There is
// one kind now, so the question moved - what a bulk route must not become is a way to
// put something where somebody else can see it, since it is the one route nobody reads
// row by row.
func TestAnImportedProjectBelongsToWhoeverImportedIt(t *testing.T) {
	a, admin, _ := startWithWorker(t)
	anna := a.signInAsUser(admin, "Anna", "anna@example.com")
	bert := a.signInAsUser(admin, "Bert", "bert@example.com")

	book, err := spreadsheet.WriteProjects("", []spreadsheet.ProjectRow{
		{Name: "Roof", StartDate: mustDay(t, "2026-08-01"), Status: "active"},
		{Name: "Cellar", StartDate: mustDay(t, "2026-08-01"), Status: "active"},
	})
	if err != nil {
		t.Fatalf("building the workbook: %v", err)
	}

	written, r := importSheet(t, anna, "/projects/import", book, "false")
	if !accepted(r.Status) {
		t.Fatalf("importing: status %d, body %.200q", r.Status, r.Body)
	}

	if written.Imported != 2 {
		t.Fatalf("wrote %d rows, want 2: %+v", written.Imported, written.Rows)
	}

	// Anna's, both of them.
	var hers struct {
		Items []projectResponse `json:"items"`
	}

	anna.must(anna.api(http.MethodGet, "/projects", nil), http.StatusOK).Data(t, &hers)

	if len(hers.Items) != 2 {
		t.Errorf("the importer sees %d project(s), want 2", len(hers.Items))
	}

	// And Bert sees neither, which is the thing the bulk route must not get around.
	var his struct {
		Items []projectResponse `json:"items"`
	}

	bert.must(bert.api(http.MethodGet, "/projects", nil), http.StatusOK).Data(t, &his)

	if len(his.Items) != 0 {
		t.Errorf("a colleague sees %d of the imported projects: %+v", len(his.Items), his.Items)
	}
}
