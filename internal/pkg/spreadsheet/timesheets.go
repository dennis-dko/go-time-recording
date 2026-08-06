// Package spreadsheet writes and reads the workbook of time entries.
//
// One package for both directions on purpose: the column order, the headings and
// how a date and an hour figure are written are the same knowledge either way, and
// an exporter and an importer that each held their own copy would drift until a
// file this application wrote could no longer be read back by it.
//
// Real .xlsx rather than comma-separated text. A CSV is what Excel mangles: the
// separator depends on the machine's locale, dates are re-interpreted on opening,
// and a description containing a semicolon quietly becomes two columns. None of
// that is recoverable at the point somebody has already saved over the file.
package spreadsheet

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

// sheetName is the one worksheet the workbook has.
//
// Named rather than left as "Sheet1" so a file that has been through Excel and
// back is still recognisable, and so the importer can say which sheet it read.
const sheetName = "Time entries"

// Row is one time entry, in the form the workbook holds it.
//
// Names rather than identifiers: a spreadsheet is edited by a person, and a column
// of user ids is not something anybody can fill in correctly. The importer resolves
// them, and says so when it cannot.
type Row struct {
	// Number is the row in the sheet this came from, so a complaint about it can
	// name the place to go and look. 1 is the heading, so data starts at 2.
	Number int

	Date        time.Time
	User        string
	Project     string
	Hours       float64
	Description string
	Status      string
}

// Columns are the headings, in order. Exported because the interface shows the
// same names in its preview, and two lists would drift.
func Columns() []string {
	return []string{"Date", "User", "Project", "Hours", "Description", "Status"}
}

// dateFormat is how a date is written and read.
//
// ISO, and stored as text rather than as an Excel date. A real date cell carries a
// serial number whose meaning depends on the workbook's epoch setting, and a
// spreadsheet that has been through a machine with a different locale can come back
// with the day and the month exchanged - silently, and only for the days where both
// readings are valid. Text is unambiguous in every locale.
const dateFormat = "2006-01-02"

// Write builds the workbook.
func Write(rows []Row) ([]byte, error) {
	book := excelize.NewFile()

	defer func() {
		// The library holds a temporary file per workbook on large sheets, so it
		// has to be closed whatever happens.
		_ = book.Close()
	}()

	index, err := book.NewSheet(sheetName)
	if err != nil {
		return nil, fmt.Errorf("creating the sheet: %w", err)
	}

	book.SetActiveSheet(index)

	// The default sheet the library starts with, which would otherwise sit beside
	// ours as an empty one somebody has to wonder about.
	if err := book.DeleteSheet("Sheet1"); err != nil {
		return nil, fmt.Errorf("removing the default sheet: %w", err)
	}

	if err := writeHeading(book); err != nil {
		return nil, err
	}

	for i, row := range rows {
		// Row 1 is the heading, so the first entry is row 2.
		if err := writeRow(book, i+2, row); err != nil {
			return nil, err
		}
	}

	// Wide enough to read without dragging every column open. Descriptions are
	// the long ones and get the most room; nothing is truncated either way, this
	// only decides what is visible on opening.
	for column, width := range map[string]float64{
		"A": 12, "B": 22, "C": 22, "D": 9, "E": 48, "F": 12,
	} {
		if err := book.SetColWidth(sheetName, column, column, width); err != nil {
			return nil, fmt.Errorf("setting the width of column %s: %w", column, err)
		}
	}

	// The heading stays visible while scrolling, which is what makes a long export
	// readable at all.
	if err := book.SetPanes(sheetName, &excelize.Panes{
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

func writeHeading(book *excelize.File) error {
	bold, err := book.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err != nil {
		return fmt.Errorf("creating the heading style: %w", err)
	}

	for i, heading := range Columns() {
		cell, cellErr := excelize.CoordinatesToCellName(i+1, 1)
		if cellErr != nil {
			return fmt.Errorf("naming a heading cell: %w", cellErr)
		}

		if err := book.SetCellStr(sheetName, cell, heading); err != nil {
			return fmt.Errorf("writing the heading %q: %w", heading, err)
		}

		if err := book.SetCellStyle(sheetName, cell, cell, bold); err != nil {
			return fmt.Errorf("styling the heading %q: %w", heading, err)
		}
	}

	return nil
}

func writeRow(book *excelize.File, number int, row Row) error {
	// Hours as a number, everything else as text. The number matters: it is what
	// lets somebody total a column in Excel, which is most of the reason to want
	// a spreadsheet rather than a list.
	values := []struct {
		text   string
		number *float64
	}{
		{text: row.Date.Format(dateFormat)},
		{text: row.User},
		{text: row.Project},
		{number: &row.Hours},
		{text: row.Description},
		{text: row.Status},
	}

	for i, value := range values {
		cell, err := excelize.CoordinatesToCellName(i+1, number)
		if err != nil {
			return fmt.Errorf("naming a cell in row %d: %w", number, err)
		}

		if value.number != nil {
			if err := book.SetCellFloat(sheetName, cell, *value.number, 2, 64); err != nil {
				return fmt.Errorf("writing row %d: %w", number, err)
			}

			continue
		}

		if err := book.SetCellStr(sheetName, cell, value.text); err != nil {
			return fmt.Errorf("writing row %d: %w", number, err)
		}
	}

	return nil
}

// ErrNoSheet is returned for a workbook with nothing this can read.
var ErrNoSheet = errors.New("the workbook has no readable sheet")

// RowError is one row that could not be understood.
//
// Kept per row rather than failing the file, so somebody who typed one date wrong
// in eighty rows is told which one rather than being told no.
type RowError struct {
	Number int
	Reason string
}

func (e RowError) Error() string {
	return fmt.Sprintf("row %d: %s", e.Number, e.Reason)
}

// Read parses a workbook back into rows.
//
// Every row it can read is returned, and every row it cannot is reported, in one
// pass: the caller decides whether a file with problems in it is worth writing, and
// cannot decide that from the first failure alone.
func Read(r io.Reader) ([]Row, []RowError, error) {
	book, err := excelize.OpenReader(r)
	if err != nil {
		return nil, nil, fmt.Errorf("reading the workbook: %w", err)
	}

	defer func() { _ = book.Close() }()

	sheet := readableSheet(book)
	if sheet == "" {
		return nil, nil, ErrNoSheet
	}

	raw, err := book.GetRows(sheet)
	if err != nil {
		return nil, nil, fmt.Errorf("reading the sheet %q: %w", sheet, err)
	}

	if len(raw) == 0 {
		return nil, nil, ErrNoSheet
	}

	rows := make([]Row, 0, len(raw))
	problems := make([]RowError, 0)

	// Row 1 is the heading. Skipped by position rather than by matching its text:
	// a heading somebody has translated is still a heading, and refusing the file
	// over it would be worse than ignoring one row.
	for i := 1; i < len(raw); i++ {
		number := i + 1

		row, rowErr := parseRow(number, raw[i])
		if rowErr != nil {
			// A row that is entirely empty is not a mistake - a spreadsheet
			// somebody has scrolled through has plenty of those at the bottom.
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

// readableSheet picks the sheet to read.
//
// Ours by name when it is there, so a workbook that also holds somebody's notes is
// read correctly; otherwise the first one, because a file assembled by hand in
// Excel has whatever name Excel gave it.
func readableSheet(book *excelize.File) string {
	for _, name := range book.GetSheetList() {
		if strings.EqualFold(name, sheetName) {
			return name
		}
	}

	if list := book.GetSheetList(); len(list) > 0 {
		return list[0]
	}

	return ""
}

// errBlankRow marks a row with nothing in it, which is skipped rather than
// reported.
var errBlankRow = errors.New("blank")

func parseRow(number int, cells []string) (Row, error) {
	// Short rows are normal: Excel does not pad a row out to the last column that
	// has a heading, so a row with no description is three cells long.
	value := func(i int) string {
		if i >= len(cells) {
			return ""
		}

		return strings.TrimSpace(cells[i])
	}

	date, user, project := value(0), value(1), value(2)
	hours, description, status := value(3), value(4), value(5)

	if date == "" && user == "" && project == "" && hours == "" &&
		description == "" && status == "" {
		return Row{}, errBlankRow
	}

	parsedDate, err := parseDate(date)
	if err != nil {
		return Row{}, err
	}

	parsedHours, err := parseHours(hours)
	if err != nil {
		return Row{}, err
	}

	return Row{
		Number:      number,
		Date:        parsedDate,
		User:        user,
		Project:     project,
		Hours:       parsedHours,
		Description: description,
		Status:      status,
	}, nil
}

// parseDate reads the date column.
//
// ISO first, because that is what this application writes. The other two forms are
// what Excel leaves behind when somebody types into the cell: the German order,
// and the serial number a cell formatted as a date turns into. Refusing those would
// mean refusing files that look perfectly correct on screen.
func parseDate(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, errors.New("the date is missing")
	}

	for _, layout := range []string{dateFormat, "02.01.2006", "01/02/2006", time.RFC3339} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed, nil
		}
	}

	// A serial number: days since the workbook's epoch. Converted by the library
	// so the epoch setting is honoured rather than assumed.
	if serial, err := strconv.ParseFloat(raw, 64); err == nil {
		if converted, convErr := excelize.ExcelDateToTime(serial, false); convErr == nil {
			return converted, nil
		}
	}

	return time.Time{}, fmt.Errorf("%q is not a date the importer understands "+
		"(use YYYY-MM-DD)", raw)
}

// parseHours reads the hours column, in either decimal convention.
//
// A comma is what a German keyboard produces and what German Excel writes, and
// reading "6,50" as nothing would reject a file that is plainly correct. Neither
// convention uses the other's separator as a thousands mark for a figure that has
// to be under 24, so accepting both is unambiguous here.
func parseHours(raw string) (float64, error) {
	if raw == "" {
		return 0, errors.New("the hours are missing")
	}

	hours, err := strconv.ParseFloat(strings.ReplaceAll(raw, ",", "."), 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a number of hours", raw)
	}

	return hours, nil
}
