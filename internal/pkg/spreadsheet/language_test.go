package spreadsheet_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/dennis-dko/go-time-recording/internal/pkg/spreadsheet"
)

// A workbook exported in one language can be imported again.
//
// The point of the whole exercise: translating the headings is only worth doing if
// the file stays usable, and the failure would be silent - a German export that
// looks perfect and is refused, or worse read one column out of step. Every
// language is checked rather than the one someone remembered to write a case for.
func TestAnExportInAnyLanguageReadsBackUnchanged(t *testing.T) {
	t.Parallel()

	day := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)

	for _, language := range append(spreadsheet.Languages(), "", "en", "de-DE") {
		t.Run("language="+language, func(t *testing.T) {
			t.Parallel()

			written, err := spreadsheet.WriteIn(language, []spreadsheet.Row{{
				Date: day, User: "Vera", Project: "Roof", Hours: 6.5, Description: "tiles",
			}})
			if err != nil {
				t.Fatalf("writing in %q: %v", language, err)
			}

			rows, problems, err := spreadsheet.Read(bytes.NewReader(written))
			if err != nil {
				t.Fatalf("reading back a %q workbook: %v", language, err)
			}

			if len(problems) != 0 {
				t.Fatalf("a workbook this package wrote in %q came back with problems: %v",
					language, problems)
			}

			if len(rows) != 1 {
				t.Fatalf("got %d rows back from a %q workbook, want 1", len(rows), language)
			}

			got := rows[0]

			if !got.Date.Equal(day) {
				t.Errorf("date came back as %v, want %v", got.Date, day)
			}

			if got.User != "Vera" || got.Project != "Roof" || got.Description != "tiles" {
				t.Errorf("a text column moved: %+v", got)
			}

			if got.Hours != 6.5 {
				t.Errorf("hours came back as %v, want 6.5", got.Hours)
			}
		})
	}
}

// The German export actually says the German words.
//
// Without this the round-trip test above would still pass if translation were
// quietly doing nothing at all.
func TestTheGermanExportIsInGerman(t *testing.T) {
	t.Parallel()

	written, err := spreadsheet.WriteIn("de", []spreadsheet.Row{{
		Date: time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC), Hours: 1,
	}})
	if err != nil {
		t.Fatalf("writing: %v", err)
	}

	book, err := excelize.OpenReader(bytes.NewReader(written))
	if err != nil {
		t.Fatalf("opening: %v", err)
	}

	defer func() { _ = book.Close() }()

	sheets := book.GetSheetList()
	if len(sheets) != 1 || sheets[0] != "Zeiteinträge" {
		t.Errorf("sheets are %v, want exactly [Zeiteinträge]", sheets)
	}

	want := []string{"Datum", "Benutzer", "Projekt", "Stunden", "Beschreibung"}

	if got := spreadsheet.ColumnsIn("de"); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("German columns are %v, want %v", got, want)
	}

	for i, heading := range want {
		cell, nameErr := excelize.CoordinatesToCellName(i+1, 1)
		if nameErr != nil {
			t.Fatalf("naming a cell: %v", nameErr)
		}

		got, readErr := book.GetCellValue(sheets[0], cell)
		if readErr != nil {
			t.Fatalf("reading %s: %v", cell, readErr)
		}

		if got != heading {
			t.Errorf("heading %s is %q, want %q", cell, got, heading)
		}
	}
}

// A sheet of projects in German reads back with its status in English, which is
// the vocabulary the rest of the application works in.
func TestProjectsSurviveTranslationOfTheirValues(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)

	for _, language := range append(spreadsheet.Languages(), "") {
		t.Run("language="+language, func(t *testing.T) {
			t.Parallel()

			written, err := spreadsheet.WriteProjects(language, []spreadsheet.ProjectRow{
				{Name: "Roof", Description: "tiles", StartDate: start, EndDate: end,
					Status: "archived"},
				{Name: "Admin", StartDate: start, Status: "active"},
			})
			if err != nil {
				t.Fatalf("writing projects in %q: %v", language, err)
			}

			rows, problems, err := spreadsheet.ReadProjects(bytes.NewReader(written))
			if err != nil {
				t.Fatalf("reading projects back: %v", err)
			}

			if len(problems) != 0 {
				t.Fatalf("problems reading back what was written: %v", problems)
			}

			if len(rows) != 2 {
				t.Fatalf("got %d project rows, want 2", len(rows))
			}

			if rows[0].Status != "archived" {
				t.Errorf("status came back as %q, want the English %q", rows[0].Status,
					"archived")
			}

			if !rows[0].EndDate.Equal(end) {
				t.Errorf("end date came back as %v, want %v", rows[0].EndDate, end)
			}

			// There were two assertions here about a Category column, telling a shared
			// project from a private one. Every project belongs to one person now, so
			// the column would have said the same thing on every row and the import
			// would have had to ignore it.
			// The one with no end date has an empty cell, not the first of January
			// in year one.
			if !rows[1].EndDate.IsZero() {
				t.Errorf("an open-ended project came back ending %v", rows[1].EndDate)
			}
		})
	}
}

// People go out and come back intact, in every language.
//
// Name, mail address, role and whether the password lives in the directory - and
// nothing else. The daily target, the ceiling and the time zone were here and are
// not: they are time figures, they belong to the person they are about, and a column
// the import would have to ignore is worse than a column that is missing.
func TestUsersSurviveTheRoundTrip(t *testing.T) {
	t.Parallel()

	for _, language := range append(spreadsheet.Languages(), "") {
		t.Run("language="+language, func(t *testing.T) {
			t.Parallel()

			written, err := spreadsheet.WriteUsers(language, []spreadsheet.UserRow{{
				Name: "Vera", Email: "vera@example.test", Role: "user",
				Directory: true,
			}})
			if err != nil {
				t.Fatalf("writing users in %q: %v", language, err)
			}

			rows, problems, err := spreadsheet.ReadUsers(bytes.NewReader(written))
			if err != nil {
				t.Fatalf("reading users back: %v", err)
			}

			if len(problems) != 0 {
				t.Fatalf("problems reading back what was written: %v", problems)
			}

			if len(rows) != 1 {
				t.Fatalf("got %d user rows, want 1", len(rows))
			}

			got := rows[0]

			if got.Email != "vera@example.test" || got.Name != "Vera" ||
				got.Role != "user" {
				t.Errorf("a column moved: %+v", got)
			}

			// Directory membership is written for information and deliberately not
			// read: an account cannot be moved into LDAP by editing a cell.
			if got.Directory {
				t.Error("the directory column was read back, which would let a " +
					"spreadsheet move an account into the directory")
			}
		})
	}
}

// A workbook of one kind is not silently read as another.
//
// Each tab has its own import now. Handing the projects importer a sheet of time
// entries has to fail loudly rather than create projects called "2026-08-03".
func TestASheetOfTheWrongKindIsRefused(t *testing.T) {
	t.Parallel()

	entries, err := spreadsheet.Write([]spreadsheet.Row{{
		Date: time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC), User: "Vera", Hours: 2,
	}})
	if err != nil {
		t.Fatalf("writing: %v", err)
	}

	rows, problems, err := spreadsheet.ReadUsers(bytes.NewReader(entries))
	if err != nil {
		// Refusing the file outright is a fine answer too.
		return
	}

	// It read something. Then every row of it has to be a complaint, not an
	// account: the mail column of a time-entry sheet holds a person's name.
	if len(rows) > 0 {
		t.Errorf("a sheet of time entries produced %d user rows: %+v", len(rows), rows)
	}

	if len(problems) == 0 {
		t.Error("a sheet of time entries was read as people without a single complaint")
	}
}
