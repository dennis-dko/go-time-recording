package spreadsheet

import (
	"errors"
	"fmt"
	"io"
	"time"
)

// projects is the sheet of projects.
var projects = Table{
	Key:      "Projects",
	Headings: []string{"Name", "Description", "Start", "End", "Status", "Category"},
	Widths:   []float64{28, 48, 12, 12, 14, 10},
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

	// Category marks a private project: a personal heading for splitting up a
	// day rather than something shared. On import it belongs to whoever imported
	// it, because a private thing cannot be given to somebody else by filling in
	// a cell.
	Category bool
}

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
			Text(translate(language, yesNo(row.Category))),
		})
	}

	return write(projects, language, cells)
}

// ReadProjects parses a workbook of projects.
func ReadProjects(r io.Reader) ([]ProjectRow, []RowError, error) {
	raw, err := read(r, projects)
	if err != nil {
		return nil, nil, err
	}

	rows := make([]ProjectRow, 0, len(raw))
	problems := make([]RowError, 0)

	for i, cells := range raw {
		number := i + 2

		row, rowErr := parseProjectRow(number, cells)
		if rowErr != nil {
			if errors.Is(rowErr, errBlankRow) {
				continue
			}

			problems = append(problems, RowError{Number: number, Reason: rowErr.Error()})

			continue
		}

		rows = append(rows, row)
	}

	return rows, problems, nil
}

func parseProjectRow(number int, cells []string) (ProjectRow, error) {
	value := cellReader(cells)

	if blank(value, len(projects.Headings)) {
		return ProjectRow{}, errBlankRow
	}

	name := value(0)
	if name == "" {
		return ProjectRow{}, errors.New("the name is missing")
	}

	category, err := parseYesNo(value(5))
	if err != nil {
		return ProjectRow{}, err
	}

	// A category has no meaningful period, so an empty start is allowed for one
	// and filled in by the service. For a project it is required: a project
	// without a start cannot be checked against the period of anything.
	start := time.Time{}

	if raw := value(2); raw != "" {
		parsed, ok := parseOptionalDate(raw)
		if !ok {
			return ProjectRow{}, fmt.Errorf("%q is not a start date the importer "+
				"understands (use YYYY-MM-DD)", raw)
		}

		start = parsed
	} else if !category {
		return ProjectRow{}, errors.New("the start date is missing")
	}

	end := time.Time{}

	if raw := value(3); raw != "" {
		parsed, ok := parseOptionalDate(raw)
		if !ok {
			return ProjectRow{}, fmt.Errorf("%q is not an end date the importer "+
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
		Status:   untranslate(value(4)),
		Category: category,
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

// parseYesNo reads a flag column, in either language and either convention.
//
// Empty is "no" rather than a complaint: a column somebody deleted from a
// hand-made file should not stop the import, and the safer reading of a missing
// "is this private" is that it is not.
func parseYesNo(raw string) (bool, error) {
	switch untranslate(raw) {
	case "", "no", "false", "0":
		return false, nil
	case "yes", "true", "1":
		return true, nil
	}

	return false, fmt.Errorf("%q is not yes or no", raw)
}
