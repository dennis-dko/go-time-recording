package spreadsheet

import (
	"io"
	"time"
)

// projects is the sheet of projects.
var projects = Table{
	Key:      "Projects",
	Headings: []string{"Name", "Description", "Start", "End", "Status"},
	Widths:   []float64{28, 48, 12, 12, 14},
}

// ProjectRow is one project as the workbook holds it.
//
// No identifier column. A spreadsheet is matched by name, which is what somebody
// editing one can actually see and type; ids would invite a file that silently
// rewrites the wrong project after being edited by hand.
type ProjectRow struct {
	// Number is the row in the sheet, so a complaint can name where to look.
	Number int

	Name        string
	Description string

	StartDate time.Time

	// EndDate is the zero time for a project with no planned end, which is the
	// ordinary case.
	EndDate time.Time

	Status string
}

// There was a Category column here, marking a private project as against a shared
// one. Every project belongs to one person now, so it would have been "yes" on every
// row - and a column that always says the same thing is a column somebody edits
// expecting something to happen. An imported project belongs to whoever imported it,
// which is the only answer there is.

// ProjectColumns are the project headings in one language.
func ProjectColumns(language string) []string { return headingsIn(language, projects) }

// WriteProjects builds the workbook of projects.
func WriteProjects(language string, rows []ProjectRow) ([]byte, error) {
	cells := make([][]Cell, 0, len(rows))

	for _, row := range rows {
		end := ""
		if !row.EndDate.IsZero() {
			end = row.EndDate.Format(dateFormat)
		}

		cells = append(cells, []Cell{
			Text(row.Name),
			Text(row.Description),
			Text(row.StartDate.Format(dateFormat)),
			Text(end),
			Text(translate(language, row.Status)),
		})
	}

	return write(projects, language, cells)
}

// ReadProjects parses a workbook of projects.
func ReadProjects(r io.Reader) ([]ProjectRow, []RowError, error) {
	return readRows(r, projects, parseProjectRow)
}

func parseProjectRow(number int, cells []string) (ProjectRow, error) {
	value := cellReader(cells)

	if blank(value, len(projects.Headings)) {
		return ProjectRow{}, errBlankRow
	}

	name := value(0)
	if name == "" {
		return ProjectRow{}, problemf("nameMissing", "the name is missing")
	}

	// An empty start is allowed and filled in by the service: a project is one
	// person's way of organising their own hours, not a plan somebody signed off, so
	// it does not need a date to begin with.
	start := time.Time{}

	if raw := value(2); raw != "" {
		parsed, ok := parseOptionalDate(raw)
		if !ok {
			return ProjectRow{}, problemf("startDate", "%q is not a start date the importer "+
				"understands (use YYYY-MM-DD)", raw)
		}

		start = parsed
	}

	end := time.Time{}

	if raw := value(3); raw != "" {
		parsed, ok := parseOptionalDate(raw)
		if !ok {
			return ProjectRow{}, problemf("endDate", "%q is not an end date the importer "+
				"understands (use YYYY-MM-DD)", raw)
		}

		end = parsed
	}

	return ProjectRow{
		Number:      number,
		Name:        name,
		Description: value(1),
		StartDate:   start,
		EndDate:     end,
		// Back to the English word the application works in, whichever language
		// the file was written in.
		Status: untranslate(value(4)),
	}, nil
}

// yesNo is how a flag is written, in words rather than as TRUE/FALSE: a
// spreadsheet is read by a person, and Excel has its own opinions about what a
// boolean cell looks like in each locale.
func yesNo(on bool) string {
	if on {
		return "yes"
	}

	return "no"
}

// parseYesNo was here, reading the Category column in either language and either
// convention. That column is gone: every project belongs to one person, so the answer
// would have been the same on every row. yesNo, which writes the word, is still used -
// the people sheet says whether a password lives in the directory.
