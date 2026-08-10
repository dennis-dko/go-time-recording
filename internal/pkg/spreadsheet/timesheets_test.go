package spreadsheet_test

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/dennis-dko/go-time-recording/internal/pkg/spreadsheet"
)

func day(t *testing.T, iso string) time.Time {
	t.Helper()

	parsed, err := time.Parse("2006-01-02", iso)
	if err != nil {
		t.Fatalf("bad test date %q: %v", iso, err)
	}

	return parsed
}

func sample(t *testing.T) []spreadsheet.Row {
	t.Helper()

	return []spreadsheet.Row{
		{
			Date: day(t, "2026-08-03"), User: "Hanne Bloem", Project: "Shared work",
			Hours: 6.5, Description: "Wrote the importer",
		},
		{
			// No project and no description, which is a perfectly ordinary entry
			// and the one a rigid parser drops.
			Date: day(t, "2026-08-04"), User: "Hanne Bloem",
			Hours: 1.25,
		},
	}
}

// What is written has to come back. Anything else and a file this application
// produced could not be fed back into it, which is the first thing anybody tries.
func TestWhatIsWrittenReadsBackUnchanged(t *testing.T) {
	written, err := spreadsheet.Write(sample(t))
	if err != nil {
		t.Fatalf("writing: %v", err)
	}

	rows, problems, err := spreadsheet.Read(bytes.NewReader(written))
	if err != nil {
		t.Fatalf("reading: %v", err)
	}

	if len(problems) > 0 {
		t.Errorf("the file it wrote has %d unreadable row(s): %v", len(problems), problems)
	}

	want := sample(t)

	if len(rows) != len(want) {
		t.Fatalf("read %d row(s), wrote %d", len(rows), len(want))
	}

	for i, got := range rows {
		if !got.Date.Equal(want[i].Date) {
			t.Errorf("row %d: date %v, want %v", i, got.Date, want[i].Date)
		}

		if got.User != want[i].User || got.Project != want[i].Project {
			t.Errorf("row %d: %q/%q, want %q/%q",
				i, got.User, got.Project, want[i].User, want[i].Project)
		}

		if got.Hours != want[i].Hours {
			t.Errorf("row %d: %v hours, want %v", i, got.Hours, want[i].Hours)
		}

		if got.Description != want[i].Description {
			t.Errorf("row %d: description %q, want %q",
				i, got.Description, want[i].Description)
		}

		// The sheet row, so a complaint can name where to look. Row 1 is the
		// heading.
		if got.Number != i+2 {
			t.Errorf("row %d reports itself as sheet row %d, want %d", i, got.Number, i+2)
		}
	}
}

// Hours have to be a number in the sheet, not text that looks like one: totalling
// the column in Excel is most of the reason to want a spreadsheet at all.
func TestHoursAreANumberInTheSheet(t *testing.T) {
	written, err := spreadsheet.Write(sample(t))
	if err != nil {
		t.Fatalf("writing: %v", err)
	}

	book, err := excelize.OpenReader(bytes.NewReader(written))
	if err != nil {
		t.Fatalf("opening: %v", err)
	}

	defer func() { _ = book.Close() }()

	sheet := book.GetSheetList()[0]

	kind, err := book.GetCellType(sheet, "D2")
	if err != nil {
		t.Fatalf("reading the cell type: %v", err)
	}

	// A number carries no type attribute in the sheet XML, so the library reports
	// it as unset; what has to be ruled out is a string type, which is what a cell
	// Excel refuses to total looks like.
	for _, text := range []excelize.CellType{
		excelize.CellTypeInlineString, excelize.CellTypeSharedString,
	} {
		if kind == text {
			t.Errorf("the hours cell is stored as text (%v), so Excel will not total "+
				"the column", kind)
		}
	}

	// And it really is the figure, not a rounded or reformatted version of it.
	value, err := book.GetCellValue(sheet, "D2")
	if err != nil {
		t.Fatalf("reading the cell: %v", err)
	}

	hours, err := strconv.ParseFloat(value, 64)
	if err != nil {
		t.Fatalf("the hours cell holds %q, which is not a number", value)
	}

	if hours != 6.5 {
		t.Errorf("the hours cell holds %v, want 6.5", hours)
	}
}

// The heading is there, spelled the way the interface spells it, and frozen.
func TestTheSheetIsReadableOnOpening(t *testing.T) {
	written, err := spreadsheet.Write(sample(t))
	if err != nil {
		t.Fatalf("writing: %v", err)
	}

	book, err := excelize.OpenReader(bytes.NewReader(written))
	if err != nil {
		t.Fatalf("opening: %v", err)
	}

	defer func() { _ = book.Close() }()

	// One sheet, ours: the library's default empty one would otherwise sit beside
	// it as something to wonder about.
	if list := book.GetSheetList(); len(list) != 1 {
		t.Errorf("the workbook has %d sheets: %v", len(list), list)
	}

	sheet := book.GetSheetList()[0]

	rows, err := book.GetRows(sheet)
	if err != nil {
		t.Fatalf("reading rows: %v", err)
	}

	if len(rows) == 0 {
		t.Fatal("the sheet is empty")
	}

	for i, heading := range spreadsheet.Columns() {
		if i >= len(rows[0]) || rows[0][i] != heading {
			t.Errorf("column %d is headed %q, want %q", i+1, cell(rows[0], i), heading)
		}
	}
}

func cell(row []string, i int) string {
	if i >= len(row) {
		return ""
	}

	return row[i]
}

// A file assembled by hand is the realistic case, and it will not be written the
// way this application writes: a German date, a comma decimal, a sheet with
// whatever name Excel gave it, and blank rows at the bottom from scrolling.
func TestAFileAssembledByHandIsUnderstood(t *testing.T) {
	book := excelize.NewFile()
	defer func() { _ = book.Close() }()

	sheet := book.GetSheetList()[0]

	for i, heading := range spreadsheet.Columns() {
		name, _ := excelize.CoordinatesToCellName(i+1, 1)
		if err := book.SetCellStr(sheet, name, heading); err != nil {
			t.Fatalf("writing a heading: %v", err)
		}
	}

	// German date order, comma decimal, no project.
	for column, value := range map[string]string{
		"A2": "04.08.2026", "B2": "Ilka Ruf", "D2": "3,75",
		"E2": "Typed into Excel",
	} {
		if err := book.SetCellStr(sheet, column, value); err != nil {
			t.Fatalf("writing %s: %v", column, err)
		}
	}

	// A row that is entirely empty, which scrolling leaves behind.
	if err := book.SetCellStr(sheet, "A4", ""); err != nil {
		t.Fatalf("writing a blank row: %v", err)
	}

	buffer, err := book.WriteToBuffer()
	if err != nil {
		t.Fatalf("writing: %v", err)
	}

	rows, problems, err := spreadsheet.Read(bytes.NewReader(buffer.Bytes()))
	if err != nil {
		t.Fatalf("reading: %v", err)
	}

	if len(problems) > 0 {
		t.Errorf("a hand-made file was rejected: %v", problems)
	}

	if len(rows) != 1 {
		t.Fatalf("read %d row(s), want the one that has anything in it", len(rows))
	}

	if got := rows[0].Date.Format("2006-01-02"); got != "2026-08-04" {
		t.Errorf("the German date read as %s", got)
	}

	if rows[0].Hours != 3.75 {
		t.Errorf("the comma decimal read as %v, want 3.75", rows[0].Hours)
	}
}

// A row nobody can make sense of is reported by its place in the sheet, and the
// rows around it still come through: one mistyped date in eighty rows should say
// which one rather than saying no.
func TestAnUnreadableRowIsNamedAndTheRestSurvive(t *testing.T) {
	book := excelize.NewFile()
	defer func() { _ = book.Close() }()

	sheet := book.GetSheetList()[0]

	for i, heading := range spreadsheet.Columns() {
		name, _ := excelize.CoordinatesToCellName(i+1, 1)
		if err := book.SetCellStr(sheet, name, heading); err != nil {
			t.Fatalf("writing a heading: %v", err)
		}
	}

	for column, value := range map[string]string{
		"A2": "2026-08-03", "B2": "Ilka", "D2": "2",
		"A3": "the third of August", "B3": "Ilka", "D3": "2",
		"A4": "2026-08-05", "B4": "Ilka", "D4": "not a number",
		"A5": "2026-08-06", "B5": "Ilka", "D5": "4",
	} {
		if err := book.SetCellStr(sheet, column, value); err != nil {
			t.Fatalf("writing %s: %v", column, err)
		}
	}

	buffer, err := book.WriteToBuffer()
	if err != nil {
		t.Fatalf("writing: %v", err)
	}

	rows, problems, err := spreadsheet.Read(bytes.NewReader(buffer.Bytes()))
	if err != nil {
		t.Fatalf("reading: %v", err)
	}

	if len(rows) != 2 {
		t.Errorf("%d readable row(s) came through, want the 2 that are fine", len(rows))
	}

	if len(problems) != 2 {
		t.Fatalf("%d problem(s) reported, want 2: %v", len(problems), problems)
	}

	// Named by their place in the sheet, which is where somebody has to go.
	if problems[0].Number != 3 || problems[1].Number != 4 {
		t.Errorf("the problems are reported for rows %d and %d, want 3 and 4",
			problems[0].Number, problems[1].Number)
	}

	// And they say what is wrong with them, not just that something is.
	if !strings.Contains(problems[0].Reason, "date") {
		t.Errorf("the date complaint does not mention the date: %q", problems[0].Reason)
	}

	if !strings.Contains(problems[1].Reason, "hours") {
		t.Errorf("the hours complaint does not mention the hours: %q", problems[1].Reason)
	}
}

// Something that is not a workbook at all is refused as a whole, which is different
// from a workbook with bad rows in it.
func TestSomethingThatIsNotAWorkbookIsRefused(t *testing.T) {
	if _, _, err := spreadsheet.Read(strings.NewReader("Date,User,Hours\n")); err == nil {
		t.Error("a CSV was accepted as a workbook")
	}

	if _, _, err := spreadsheet.Read(bytes.NewReader(nil)); err == nil {
		t.Error("an empty file was accepted as a workbook")
	}
}

// An export of nothing is still a valid workbook with its heading, rather than an
// error: a filter that matched nothing is an answer.
func TestAnEmptyExportIsStillAWorkbook(t *testing.T) {
	written, err := spreadsheet.Write(nil)
	if err != nil {
		t.Fatalf("writing: %v", err)
	}

	rows, problems, err := spreadsheet.Read(bytes.NewReader(written))
	if err != nil {
		t.Fatalf("reading: %v", err)
	}

	if len(rows) != 0 || len(problems) != 0 {
		t.Errorf("an empty export read back as %d row(s) and %d problem(s)",
			len(rows), len(problems))
	}
}
