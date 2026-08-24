package spreadsheet

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/xuri/excelize/v2"
)

// Table describes one kind of sheet: what it is called and what its columns are.
//
// Extracted when projects and users grew exports of their own. The three sheets
// differ only in their columns and how a row is turned into cells; the freeze
// pane, the bold heading, the temporary file the library keeps and the business
// of picking a sheet to read are the same for all of them, and were worth having
// once rather than three times.
type Table struct {
	// Key is what the sheet is called in English, and the key its name is
	// translated under.
	Key string

	// Headings are the column names in English, in order. Translated on the way
	// out; on the way in the heading row is skipped by position, so a file whose
	// headings are in another language still reads.
	Headings []string

	// Widths is how wide each column opens, in the same order. Nothing is
	// truncated by a narrow column - this only decides what is legible before
	// anybody drags a column open.
	Widths []float64
}

// Cell is one value in a row.
//
// Text or number, because the distinction is visible in the result: a column of
// numbers can be summed in Excel, which is most of the reason to want a
// spreadsheet rather than a list. A number written as text cannot.
type Cell struct {
	Text   string
	Number *float64
}

// Text makes a text cell.
func Text(s string) Cell { return Cell{Text: s} }

// Number makes a numeric cell.
func Number(v float64) Cell { return Cell{Number: &v} }

// write builds a workbook of one sheet.
func write(table Table, language string, rows [][]Cell) ([]byte, error) {
	book := excelize.NewFile()

	defer func() {
		// The library holds a temporary file per workbook on large sheets, so it
		// has to be closed whatever happens.
		_ = book.Close()
	}()

	sheet := translate(language, table.Key)

	index, err := book.NewSheet(sheet)
	if err != nil {
		return nil, fmt.Errorf("creating the sheet: %w", err)
	}

	book.SetActiveSheet(index)

	// The default sheet the library starts with, which would otherwise sit beside
	// ours as an empty one somebody has to wonder about.
	if err := book.DeleteSheet("Sheet1"); err != nil {
		return nil, fmt.Errorf("removing the default sheet: %w", err)
	}

	if err := writeHeading(book, sheet, headingsIn(language, table)); err != nil {
		return nil, err
	}

	for i, row := range rows {
		// Row 1 is the heading, so the first entry is row 2.
		if err := writeRow(book, sheet, i+2, row); err != nil {
			return nil, err
		}
	}

	for i, width := range table.Widths {
		column, nameErr := excelize.ColumnNumberToName(i + 1)
		if nameErr != nil {
			return nil, fmt.Errorf("naming column %d: %w", i+1, nameErr)
		}

		if err := book.SetColWidth(sheet, column, column, width); err != nil {
			return nil, fmt.Errorf("setting the width of column %s: %w", column, err)
		}
	}

	// The heading stays visible while scrolling, which is what makes a long
	// export readable at all.
	if err := book.SetPanes(sheet, &excelize.Panes{
		Freeze: true, Split: false, XSplit: 0, YSplit: 1,
		TopLeftCell: "A2", ActivePane: "bottomLeft",
	}); err != nil {
		return nil, fmt.Errorf("freezing the heading: %w", err)
	}

	buffer, err := book.WriteToBuffer()
	if err != nil {
		return nil, fmt.Errorf("writing the workbook: %w", err)
	}

	return buffer.Bytes(), nil
}

func writeHeading(book *excelize.File, sheet string, headings []string) error {
	bold, err := book.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err != nil {
		return fmt.Errorf("creating the heading style: %w", err)
	}

	for i, heading := range headings {
		cell, cellErr := excelize.CoordinatesToCellName(i+1, 1)
		if cellErr != nil {
			return fmt.Errorf("naming a heading cell: %w", cellErr)
		}

		if err := book.SetCellStr(sheet, cell, heading); err != nil {
			return fmt.Errorf("writing the heading %q: %w", heading, err)
		}

		if err := book.SetCellStyle(sheet, cell, cell, bold); err != nil {
			return fmt.Errorf("styling the heading %q: %w", heading, err)
		}
	}

	return nil
}

func writeRow(book *excelize.File, sheet string, number int, cells []Cell) error {
	for i, value := range cells {
		name, err := excelize.CoordinatesToCellName(i+1, number)
		if err != nil {
			return fmt.Errorf("naming a cell in row %d: %w", number, err)
		}

		if value.Number != nil {
			if err := book.SetCellFloat(sheet, name, *value.Number, 2, 64); err != nil {
				return fmt.Errorf("writing row %d: %w", number, err)
			}

			continue
		}

		if err := book.SetCellStr(sheet, name, value.Text); err != nil {
			return fmt.Errorf("writing row %d: %w", number, err)
		}
	}

	return nil
}

// ErrNoSheet is returned for a workbook with nothing this can read.
var ErrNoSheet = errors.New("the workbook has no readable sheet")

// ErrWrongSheet is a workbook of the right shape and the wrong kind.
//
// Each tab has its own import, so handing the people importer a sheet of time
// entries is now an easy mistake to make - two clicks apart, and the files look
// alike in a folder. Without this it half worked: the reader fell back to the
// first sheet, and a column of dates was read as a column of names. Refusing it
// by name is possible because this application wrote the file and named the
// sheet.
var ErrWrongSheet = errors.New("this workbook holds something else")

// tables are every sheet this package knows, which is what lets it recognise a
// workbook of the wrong kind rather than reading one column out of step.
func tables() []Table { return []Table{timesheets, projects, users, roles} }

// RowError is one row that could not be understood.
//
// Kept per row rather than failing the file, so somebody who typed one date
// wrong in eighty rows is told which one rather than being told no.
type RowError struct {
	Number int
	Reason string

	// Code names which complaint this is, and Values are what its sentence
	// interpolated, so the interface can say the same thing in the reader's
	// language. Reason stays as the English wording: it is what a log wants, and
	// what a client with no translation for the code falls back to.
	//
	// This is the same arrangement the refusals from the API use, for the same
	// reason - the preview translates its headings and its cells, and a German
	// reader was comparing them against a complaint written in English.
	Code   string
	Values []any
}

func (e RowError) Error() string {
	return fmt.Sprintf("row %d: %s", e.Number, e.Reason)
}

// rowProblem is a reason a row cannot be used, carrying the code with it.
//
// The parsers return plain errors and the readers turn them into RowErrors, so
// the code has to travel as part of the error rather than beside it.
type rowProblem struct {
	code    string
	values  []any
	message string
}

func (p rowProblem) Error() string { return p.message }

// problemf builds one, formatting the English wording from the same values the
// translated sentence will interpolate - so {0} in German is what %q was in
// English, and neither can drift from the other.
func problemf(code, format string, values ...any) error {
	return rowProblem{code: code, values: values, message: fmt.Sprintf(format, values...)}
}

// Problemf is problemf for the service layer.
//
// Some complaints about a row cannot be made here: whether a project may be
// archived, or whether an account exists, is not something a reader of
// spreadsheets knows. They are still complaints about a row and they end up in
// the same preview column, so they are built the same way rather than through a
// second arrangement that would have to be kept level with this one.
func Problemf(code, format string, values ...any) error {
	return problemf(code, format, values...)
}

// ProblemOf reads the code and the interpolated values off a row complaint.
//
// Both are empty for an error that does not carry them, which leaves the caller
// with the English wording - the same fallback the interface makes for a code
// nobody has translated.
func ProblemOf(err error) (code string, values []any) {
	if problem, ok := errors.AsType[rowProblem](err); ok {
		return problem.code, problem.values
	}

	return "", nil
}

// rowErrorFor reports a row, with the code if the reason carried one.
func rowErrorFor(number int, err error) RowError {
	out := RowError{Number: number, Reason: err.Error()}

	if problem, ok := errors.AsType[rowProblem](err); ok {
		out.Code, out.Values = problem.code, problem.values
	}

	return out
}

// errBlankRow marks a row with nothing in it, which is skipped rather than
// reported.
var errBlankRow = errors.New("blank")

// readWithHeading returns the heading row as well as the data rows.
//
// Only the sheet of roles needs it. Every other sheet has a fixed set of columns
// and can be read by position; that one's columns are the permissions the
// application enforces, and that list grows between releases - so which right a
// column stands for is a question only its heading can answer.
func readWithHeading(r io.Reader, table Table) ([]string, [][]string, error) {
	raw, err := rowsOf(r, table)
	if err != nil {
		return nil, nil, err
	}

	return raw[0], raw[1:], nil
}

// read returns the data rows of a workbook, the heading dropped.
// readRows is what all three readers do: read the sheet, parse every row, and
// keep the rows that failed beside the rows that did not - an import that stops
// at the first bad line tells somebody with a hundred rows about one of them.
//
// It was these twenty lines three times over, differing in the row type, the
// table and the parser, which is exactly the list of parameters below. The
// blank-row rule in the middle is the reason that mattered: it is a decision
// about what an empty line in a spreadsheet means, and it was written down in
// three places.
func readRows[T any](r io.Reader, table Table,
	parse func(int, []string) (T, error)) ([]T, []RowError, error) {

	raw, err := read(r, table)
	if err != nil {
		return nil, nil, err
	}

	rows := make([]T, 0, len(raw))
	problems := make([]RowError, 0)

	for i, cells := range raw {
		// The heading was row 1 and has been dropped, so the first data row is 2.
		number := i + 2

		row, rowErr := parse(number, cells)
		if rowErr != nil {
			// A blank line is not a problem to report. Spreadsheets collect them
			// at the bottom, and somebody who deleted a row would be told off for
			// it.
			if errors.Is(rowErr, errBlankRow) {
				continue
			}

			problems = append(problems, rowErrorFor(number, rowErr))

			continue
		}

		rows = append(rows, row)
	}

	return rows, problems, nil
}

func read(r io.Reader, table Table) ([][]string, error) {
	raw, err := rowsOf(r, table)
	if err != nil {
		return nil, err
	}

	// Row 1 is the heading. Dropped by position rather than by matching its text:
	// a heading somebody has translated - or that this application itself wrote in
	// another language - is still a heading, and refusing the file over it would
	// be worse than ignoring one row.
	return raw[1:], nil
}

// rowsOf opens a workbook and returns every row of the sheet that belongs to this
// table, heading included.
func rowsOf(r io.Reader, table Table) ([][]string, error) {
	book, err := excelize.OpenReader(r)
	if err != nil {
		return nil, fmt.Errorf("reading the workbook: %w", err)
	}

	defer func() { _ = book.Close() }()

	sheet, err := readableSheet(book, table)
	if err != nil {
		return nil, err
	}

	raw, err := book.GetRows(sheet)
	if err != nil {
		return nil, fmt.Errorf("reading the sheet %q: %w", sheet, err)
	}

	if len(raw) == 0 {
		return nil, ErrNoSheet
	}

	return raw, nil
}

// readableSheet picks the sheet to read.
//
// Ours by name in any language it can be called, so a workbook that also holds
// somebody's notes is read from the right sheet whichever language it was exported
// in. Failing that the first sheet, because a file assembled by hand in Excel has
// whatever name Excel gave it - unless the sheets are named after one of the other
// kinds, which is not an unnamed file but the wrong file.
func readableSheet(book *excelize.File, table Table) (string, error) {
	list := book.GetSheetList()

	for _, wanted := range names(table.Key) {
		for _, name := range list {
			if strings.EqualFold(name, wanted) {
				return name, nil
			}
		}
	}

	if len(list) == 0 {
		return "", ErrNoSheet
	}

	for _, other := range tables() {
		if other.Key == table.Key {
			continue
		}

		for _, wanted := range names(other.Key) {
			for _, name := range list {
				if strings.EqualFold(name, wanted) {
					return "", fmt.Errorf("%w: it is a sheet of %q, and this reads %q",
						ErrWrongSheet, other.Key, table.Key)
				}
			}
		}
	}

	return list[0], nil
}

// cellReader reads a row's cells by position, tolerating a short row.
//
// Short rows are normal: Excel does not pad a row out to the last column that
// has a heading, so a row with no description is three cells long.
func cellReader(cells []string) func(int) string {
	return func(i int) string {
		if i >= len(cells) {
			return ""
		}

		return strings.TrimSpace(cells[i])
	}
}

// blank reports whether every cell of a row is empty, which is skipped rather
// than reported: a spreadsheet somebody has scrolled through has plenty of those
// at the bottom.
func blank(value func(int) string, columns int) bool {
	for i := range columns {
		if value(i) != "" {
			return false
		}
	}

	return true
}
