//go:build integration

package integration

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/dennis-dko/go-time-recording/internal/pkg/spreadsheet"
)

// Time entries go in and out as a spreadsheet.
//
// Export is the easy half. Import writes many rows from a file somebody assembled
// by hand, so what these tests are really about is what it refuses: a row for
// somebody else's account, a day that would go over the ceiling, and a file with
// one bad row in it - which has to leave the database exactly as it was.

type importRow struct {
	Row         int     `json:"row"`
	Date        string  `json:"date"`
	User        string  `json:"user"`
	Project     string  `json:"project"`
	Hours       float64 `json:"hours"`
	Description string  `json:"description"`
	Problem     string  `json:"problem"`
}

type importResult struct {
	DryRun   bool        `json:"dryRun"`
	Rows     []importRow `json:"rows"`
	Writable int         `json:"writable"`
	Rejected int         `json:"rejected"`
	Imported int         `json:"imported"`
}

// mustDay parses a date the tests write inline.
func mustDay(t *testing.T, iso string) time.Time {
	t.Helper()

	parsed, err := time.Parse("2006-01-02", iso)
	if err != nil {
		t.Fatalf("bad test date %q: %v", iso, err)
	}

	return parsed
}

// workbookOf builds an .xlsx through the same writer the export uses, which is what
// a person uploading a file they exported and edited would have.
func workbookOf(t *testing.T, rows ...spreadsheet.Row) []byte {
	t.Helper()

	book, err := spreadsheet.Write(rows)
	if err != nil {
		t.Fatalf("building the workbook: %v", err)
	}

	return book
}

func importFile(t *testing.T, c *client, book []byte, dryRun string) (importResult, response) {
	t.Helper()

	r := c.upload("/timesheets/import", "file", "entries.xlsx", book,
		map[string]string{"dryRun": dryRun})

	var out importResult

	if r.Status == http.StatusOK || r.Status == http.StatusCreated {
		r.Data(t, &out)
	}

	return out, r
}

func TestExportingAndImportingRoundTrips(t *testing.T) {
	t.Parallel()

	a, admin, _ := startWithWorker(t)
	other := a.signInAsUser(admin, "Mika", "mika@example.com")

	var shared projectResponse
	other.must(other.api(http.MethodPost, "/projects", map[string]any{
		"name": "Shared work", "startDate": "2026-08-01",
	}), http.StatusCreated, http.StatusOK).Data(t, &shared)

	// Two entries of their own, one with a project and one without.
	for _, entry := range []map[string]any{
		{"date": "2026-08-03", "durationHours": 6.5, "projectId": shared.ID,
			"description": "Exported and imported"},
		{"date": "2026-08-04", "durationHours": 1.25},
	} {
		other.must(other.api(http.MethodPost, "/timesheets", entry),
			http.StatusCreated, http.StatusOK)
	}

	exported := other.must(other.api(http.MethodGet,
		"/timesheets/export?from=2026-08-01&to=2026-08-31", nil), http.StatusOK)

	// A real workbook: a zip, which is what an .xlsx is, and readable by the same
	// reader the import uses.
	if !bytes.HasPrefix(exported.Body, []byte("PK")) {
		t.Fatalf("the export is not a zip archive: %.20q", exported.Body)
	}

	rows, problems, err := spreadsheet.Read(bytes.NewReader(exported.Body))
	if err != nil {
		t.Fatalf("the export cannot be read back: %v", err)
	}

	if len(problems) > 0 {
		t.Errorf("the export has unreadable rows: %v", problems)
	}

	if len(rows) != 2 {
		t.Fatalf("the export has %d row(s), want 2", len(rows))
	}

	// The names, not the ids: a column of user ids is not something anybody can
	// fill in correctly by hand.
	if rows[0].User != "Mika" {
		t.Errorf("the export names the user as %q, want Mika", rows[0].User)
	}

	if rows[0].Project != "Shared work" {
		t.Errorf("the export names the project as %q", rows[0].Project)
	}

	if rows[0].Hours != 6.5 {
		t.Errorf("the export has %v hours, want 6.5", rows[0].Hours)
	}

	// And the same file goes back in, which is the first thing anybody tries.
	result, r := importFile(t, other, exported.Body, "false")
	if r.Status != http.StatusOK && r.Status != http.StatusCreated {
		t.Fatalf("importing the export answered %d: %s", r.Status, r.Body)
	}

	if result.Imported != 2 {
		t.Errorf("%d entries were imported, want 2", result.Imported)
	}

	// Four now: the two that were there and the two from the file. An import is a
	// creation, not a merge - it has no way to know which entry a row means.
	var listed listOf[timesheetResponse]
	other.must(other.api(http.MethodGet, "/timesheets?from=2026-08-01&to=2026-08-31", nil),
		http.StatusOK).Data(t, &listed)

	if len(listed.Items) != 4 {
		t.Errorf("there are %d entries after the import, want 4", len(listed.Items))
	}
}

// A German export is in German, and goes back in.
//
// The projects and users exports read the lang parameter from the start; this one
// dropped it between the handler and the writer, so a German screen produced a file
// headed Date, User, Project, Hours. Nothing failed - the importer accepts either
// language - which is exactly why nobody noticed.
func TestTheGermanTimeExportIsGermanAndStillImports(t *testing.T) {
	t.Parallel()

	_, _, worker := startWithWorker(t)

	worker.must(worker.api(http.MethodPost, "/timesheets", map[string]any{
		"date": "2026-08-03", "durationHours": 6.5, "description": "in German",
	}), http.StatusCreated, http.StatusOK)

	exported := worker.must(worker.api(http.MethodGet, "/timesheets/export?lang=de", nil),
		http.StatusOK)

	// The heading row itself, because the reader accepts both languages: parsing
	// the file successfully proves the round trip and nothing at all about which
	// language it came back in.
	assertGermanHeadings(t, exported.Body)

	rows, problems, err := spreadsheet.Read(bytes.NewReader(exported.Body))
	if err != nil {
		t.Fatalf("reading back a German export: %v", err)
	}

	if len(problems) != 0 {
		t.Fatalf("a German export came back with problems: %v", problems)
	}

	if len(rows) != 1 || rows[0].Hours != 6.5 {
		t.Fatalf("the German export read back as %+v", rows)
	}

	result, r := importFile(t, worker, exported.Body, "false")
	if !accepted(r.Status) {
		t.Fatalf("importing a German export answered %d: %s", r.Status, r.Body)
	}

	if result.Imported != 1 {
		t.Errorf("%d rows were imported from the German export, want 1", result.Imported)
	}
}

// assertGermanHeadings opens the workbook and reads row 1.
//
// Opened rather than searched: an .xlsx is a zip, so the words are not in the
// bytes at all and looking for them there finds nothing whatever the file says.
func assertGermanHeadings(t *testing.T, workbook []byte) {
	t.Helper()

	book, err := excelize.OpenReader(bytes.NewReader(workbook))
	if err != nil {
		t.Fatalf("opening the export: %v", err)
	}

	defer func() { _ = book.Close() }()

	sheets := book.GetSheetList()
	if len(sheets) == 0 {
		t.Fatal("the export has no sheet")
	}

	if sheets[0] != "Zeiteinträge" {
		t.Errorf("the sheet is called %q, want Zeiteinträge", sheets[0])
	}

	rows, err := book.GetRows(sheets[0])
	if err != nil || len(rows) == 0 {
		t.Fatalf("reading the heading row: %v", err)
	}

	want := spreadsheet.ColumnsIn("de")
	if strings.Join(rows[0], ",") != strings.Join(want, ",") {
		t.Errorf("the headings are %v, want %v", rows[0], want)
	}
}

// The preview writes nothing. That is the whole point of it: somebody with a file
// of eighty rows should see what it would do before it does it.
func TestThePreviewWritesNothing(t *testing.T) {
	t.Parallel()

	_, _, worker := startWithWorker(t)

	book := workbookOf(t, spreadsheet.Row{
		Date: mustDay(t, "2026-08-03"), Hours: 3, Description: "Only a look",
	})

	result, r := importFile(t, worker, book, "true")
	if r.Status != http.StatusOK && r.Status != http.StatusCreated {
		t.Fatalf("the preview answered %d: %s", r.Status, r.Body)
	}

	if !result.DryRun {
		t.Error("the preview does not report itself as one")
	}

	if result.Imported != 0 {
		t.Errorf("the preview imported %d entries", result.Imported)
	}

	if result.Writable != 1 || result.Rejected != 0 {
		t.Errorf("the preview counts %d writable and %d rejected, want 1 and 0",
			result.Writable, result.Rejected)
	}

	// And the row is described, so the preview can be read rather than trusted.
	if len(result.Rows) != 1 {
		t.Fatalf("the preview describes %d row(s), want 1", len(result.Rows))
	}

	if result.Rows[0].Row != 2 {
		t.Errorf("the row is reported as sheet row %d, want 2 (1 is the heading)",
			result.Rows[0].Row)
	}

	if result.Rows[0].Hours != 3 {
		t.Errorf("the row shows %v hours, want 3", result.Rows[0].Hours)
	}

	var listed listOf[timesheetResponse]
	worker.must(worker.api(http.MethodGet, "/timesheets", nil), http.StatusOK).Data(t, &listed)

	if len(listed.Items) != 0 {
		t.Errorf("the preview created %d entries", len(listed.Items))
	}
}

// One bad row and nothing is written. A file half-imported leaves nobody able to
// say which half, or which entries came from it.
func TestAFileWithABadRowImportsNothing(t *testing.T) {
	t.Parallel()

	_, _, worker := startWithWorker(t)

	book := workbookOf(t,
		spreadsheet.Row{Date: mustDay(t, "2026-08-03"), Hours: 2, Description: "fine"},
		// 30 hours in a day, which no ceiling allows and the API refuses one entry
		// at a time.
		spreadsheet.Row{Date: mustDay(t, "2026-08-04"), Hours: 30, Description: "not fine"},
		spreadsheet.Row{Date: mustDay(t, "2026-08-05"), Hours: 2, Description: "also fine"},
	)

	result, r := importFile(t, worker, book, "false")
	if r.Status != http.StatusConflict {
		t.Fatalf("a file with a bad row answered %d, want 409: %s", r.Status, r.Body)
	}

	_ = result

	// Nothing at all, including the two rows that were perfectly good.
	var listed listOf[timesheetResponse]
	worker.must(worker.api(http.MethodGet, "/timesheets", nil), http.StatusOK).Data(t, &listed)

	if len(listed.Items) != 0 {
		t.Errorf("%d entries were written from a file that was refused", len(listed.Items))
	}

	// The preview says which row, so it can be fixed.
	preview, _ := importFile(t, worker, book, "true")

	if preview.Rejected != 1 || preview.Writable != 2 {
		t.Fatalf("the preview counts %d rejected and %d writable, want 1 and 2",
			preview.Rejected, preview.Writable)
	}

	var named bool

	for _, row := range preview.Rows {
		if row.Problem == "" {
			continue
		}

		named = true

		// Row 3 in the sheet: the heading, then the good row, then this one.
		if row.Row != 3 {
			t.Errorf("the bad row is reported as sheet row %d, want 3", row.Row)
		}

		if !strings.Contains(strings.ToLower(row.Problem), "hour") &&
			!strings.Contains(strings.ToLower(row.Problem), "limit") {
			t.Errorf("the problem does not say what is wrong: %q", row.Problem)
		}
	}

	if !named {
		t.Error("the preview reported no problem for a file that was refused")
	}
}

// A file naming somebody else is refused, and the refusal names the row rather than
// the whole file.
//
// This used to end by showing the way past it: an account holding
// timesheets:write:all imported the same file and got both rows. There is no such
// account any more and no right to build one from, so the second half of this case is
// now the opposite claim - the most privileged account this application has is refused
// the same row for the same reason.
//
// A per-row refusal rather than a rejected file, because a spreadsheet somebody
// exported from a colleague's screen and edited is a realistic thing to be handed:
// the rows that are yours import, and the ones that are not come back named.
func TestImportingSomebodyElsesRowIsRefused(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	admin.must(admin.api(http.MethodPost, "/users", map[string]any{
		"name": "Vera", "email": "vera@example.com",
		"role": "user", "password": "vera-password-1",
	}), http.StatusCreated, http.StatusOK)

	admin.must(admin.api(http.MethodPost, "/users", map[string]any{
		"name": "Wim", "email": "wim@example.com",
		"role": "user", "password": "wim-password-1",
	}), http.StatusCreated, http.StatusOK)

	vera := a.newClient()
	vera.signIn("vera@example.com", "vera-password-1")

	book := workbookOf(t,
		spreadsheet.Row{Date: mustDay(t, "2026-08-03"), User: "Vera", Hours: 2},
		spreadsheet.Row{Date: mustDay(t, "2026-08-04"), User: "Wim", Hours: 2},
	)

	preview, r := importFile(t, vera, book, "true")
	if r.Status != http.StatusOK && r.Status != http.StatusCreated {
		t.Fatalf("the preview answered %d: %s", r.Status, r.Body)
	}

	if preview.Writable != 1 || preview.Rejected != 1 {
		t.Errorf("the preview counts %d writable and %d rejected, want 1 and 1",
			preview.Writable, preview.Rejected)
	}

	for _, row := range preview.Rows {
		if row.Row == 3 && !strings.Contains(row.Problem, "own time") {
			t.Errorf("the refusal for somebody else's row says %q", row.Problem)
		}
	}

	// And it really is refused, not merely previewed as such.
	if _, r := importFile(t, vera, book, "false"); r.Status != http.StatusConflict {
		t.Errorf("importing somebody else's row answered %d, want 409", r.Status)
	}

	// And nobody may. The caller here both works and administers, which is as far as
	// any role reaches - so this is not "Vera lacks a right somebody else has", it is
	// that the right is not there to be held.
	mira := a.signInAsWorkingAdmin(admin, "Mira", "mira@example.com")

	elsewhere, r := importFile(t, mira, book, "true")
	if r.Status != http.StatusOK && r.Status != http.StatusCreated {
		t.Fatalf("the preview answered %d for an account that administers too: %s",
			r.Status, r.Body)
	}

	// Two rows named for two other people, so both are rejected and none is
	// writable - Mira is neither Vera nor Wim.
	if elsewhere.Writable != 0 || elsewhere.Rejected != 2 {
		t.Errorf("an account that administers as well previews %d writable and %d "+
			"rejected, want 0 and 2: administering the installation is not a way into "+
			"a colleague's hours", elsewhere.Writable, elsewhere.Rejected)
	}

	if _, r := importFile(t, mira, book, "false"); r.Status != http.StatusConflict {
		t.Errorf("an account that administers as well imported somebody else's rows "+
			"with %d, want 409", r.Status)
	}
}

// Something that is not a workbook is refused as a whole, with a reason - as
// opposed to a workbook with bad rows, which is refused row by row.
func TestSomethingThatIsNotAWorkbookIsRefusedOutright(t *testing.T) {
	t.Parallel()

	_, _, worker := startWithWorker(t)

	r := worker.upload("/timesheets/import", "file", "entries.xlsx",
		[]byte("Date;User;Hours\n2026-08-03;Admin;2\n"), map[string]string{"dryRun": "true"})

	if r.Status != http.StatusBadRequest {
		t.Errorf("a CSV answered %d, want 400: %s", r.Status, r.Body)
	}

	if !strings.Contains(strings.ToLower(r.Message()), "xlsx") {
		t.Errorf("the refusal does not say what was expected: %q", r.Message())
	}
}

// A workbook of the wrong kind is a different refusal from a file that is not a
// workbook at all.
//
// The two exports leave the browser as .xlsx files that look identical in a file
// manager, and picking the wrong one is the ordinary mistake here - the projects
// sheet uploaded to the time entries importer. Answering "this is not a readable
// .xlsx workbook" sends whoever did that looking for a corrupt file, when the
// file is perfectly good and only needs uploading somewhere else. The reader
// already knows which sheet it found and which it wanted; wrongWorkbook is how
// that reaches the screen.
func TestAWorkbookOfTheWrongKindSaysWhichKindItIs(t *testing.T) {
	t.Parallel()

	_, _, worker := startWithWorker(t)

	book, err := spreadsheet.WriteProjects("", []spreadsheet.ProjectRow{
		{Name: "Roof", Status: "active"},
	})
	if err != nil {
		t.Fatalf("building the projects workbook: %v", err)
	}

	r := worker.upload("/timesheets/import", "file", "projects.xlsx", book,
		map[string]string{"dryRun": "true"})

	if r.Status != http.StatusBadRequest {
		t.Fatalf("a projects workbook answered %d, want 400: %s", r.Status, r.Body)
	}

	if code := errorCode(t, r); code != "wrongWorkbook" {
		t.Errorf("a projects workbook was refused as %q, want wrongWorkbook: %s",
			code, r.Message())
	}
}

// An export holds the caller's own entries and nobody else's, the same way the list
// does - an export that saw more than the screen would be a way around the screen.
//
// This used to prove the scoping by contrast: an account holding timesheets:read:all
// exported both rows, so a one-row export was clearly a scope and not an accident. No
// account can do that now, so the contrast is drawn the other way round - each of the
// two accounts exports exactly its own row, which distinguishes a scope from an empty
// answer just as well and does not need a right that no longer exists.
// And the export answers the same rows the list does for the same filter.
//
// The user scoping is proved above, and it was the only half being proved. The
// other three parameters - the project, and the two ends of the date range - were
// read by two functions that happened to parse them identically, with a comment
// on one claiming the other called it. They are one function now, and this is
// what would notice if they stopped being: an export that ignored `from` hands
// over months of entries to whoever asked for a week of them.
func TestAnExportHonoursTheSameFilterTheListDoes(t *testing.T) {
	t.Parallel()

	_, _, worker := startWithWorker(t)

	for _, day := range []string{"2026-08-03", "2026-08-04", "2026-08-05"} {
		worker.must(worker.api(http.MethodPost, "/timesheets", map[string]any{
			"date": day, "durationHours": 2, "description": day,
		}), http.StatusCreated, http.StatusOK)
	}

	const window = "?from=2026-08-04&to=2026-08-05"

	var listed struct {
		Items []struct {
			Description string `json:"description"`
		} `json:"items"`
		TotalCount int `json:"totalCount"`
	}

	worker.must(worker.api(http.MethodGet, "/timesheets"+window, nil),
		http.StatusOK).Data(t, &listed)

	if listed.TotalCount != 2 {
		t.Fatalf("the list answered %d entries for the window, want 2", listed.TotalCount)
	}

	exported := worker.must(worker.api(http.MethodGet, "/timesheets/export"+window, nil),
		http.StatusOK)

	rows, _, err := spreadsheet.Read(bytes.NewReader(exported.Body))
	if err != nil {
		t.Fatalf("reading the export: %v", err)
	}

	if len(rows) != len(listed.Items) {
		t.Fatalf("the export has %d row(s) and the list %d for the same filter",
			len(rows), len(listed.Items))
	}

	inExport := map[string]bool{}
	for _, row := range rows {
		inExport[row.Description] = true
	}

	for _, item := range listed.Items {
		if !inExport[item.Description] {
			t.Errorf("the list answered %q and the export did not", item.Description)
		}
	}

	if inExport["2026-08-03"] {
		t.Error("the export ignored 'from' and handed over an entry outside the window")
	}
}

func TestAnExportIsScopedTheSameWayTheListIs(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")
	other := a.signInAsUser(admin, "Malin", "malin@example.com")

	admin.must(admin.api(http.MethodPost, "/users", map[string]any{
		"name": "Xenia", "email": "xenia@example.com",
		"role": "user", "password": "xenia-password-1",
	}), http.StatusCreated, http.StatusOK)

	xenia := a.newClient()
	xenia.signIn("xenia@example.com", "xenia-password-1")

	xenia.must(xenia.api(http.MethodPost, "/timesheets", map[string]any{
		"date": "2026-08-03", "durationHours": 4, "description": "hers",
	}), http.StatusCreated, http.StatusOK)

	other.must(other.api(http.MethodPost, "/timesheets", map[string]any{
		"date": "2026-08-03", "durationHours": 5, "description": "the other one's",
	}), http.StatusCreated, http.StatusOK)

	// Hers alone with no filter at all, which is where an export that ignored the
	// scoping would hand her the whole instance.
	exported := xenia.must(xenia.api(http.MethodGet, "/timesheets/export", nil),
		http.StatusOK)

	rows, _, err := spreadsheet.Read(bytes.NewReader(exported.Body))
	if err != nil {
		t.Fatalf("reading her export: %v", err)
	}

	if len(rows) != 1 {
		t.Fatalf("her export has %d row(s), want only her own", len(rows))
	}

	if rows[0].User != "Xenia" {
		t.Errorf("her export contains %q's entry", rows[0].User)
	}

	// And naming somebody else is refused rather than quietly answered with her
	// own - the same answer the entry list gives.
	var people struct {
		Items []userResponse `json:"items"`
	}

	admin.must(admin.api(http.MethodGet, "/users", nil), http.StatusOK).Data(t, &people)

	var otherID uint

	for _, person := range people.Items {
		if person.Email == "malin@example.com" {
			otherID = person.ID
		}
	}

	if otherID == 0 {
		t.Fatal("could not find the other account's id")
	}

	if got := xenia.api(http.MethodGet,
		path("/timesheets/export?userId=", otherID), nil).Status; got == http.StatusOK {
		t.Error("she exported somebody else's entries by asking for them by id")
	}

	// And the other account's export is its own single row, not hers and not both.
	// That is what makes a one-row export a scope rather than an export that happens
	// to be short.
	theirs := other.must(other.api(http.MethodGet, "/timesheets/export", nil), http.StatusOK)

	theirRows, _, err := spreadsheet.Read(bytes.NewReader(theirs.Body))
	if err != nil {
		t.Fatalf("reading the other export: %v", err)
	}

	if len(theirRows) != 1 {
		t.Fatalf("the other export has %d row(s), want only its own", len(theirRows))
	}

	if theirRows[0].User != "Malin" {
		t.Errorf("the other export contains %q's entry", theirRows[0].User)
	}
}
