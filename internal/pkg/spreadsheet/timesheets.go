// Package spreadsheet writes and reads the workbooks the application exchanges.
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
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

// timesheets is the sheet of time entries.
var timesheets = Table{
	Key:      "Time entries",
	Headings: []string{"Date", "User", "Project", "Hours", "Description"},
	Widths:   []float64{12, 22, 22, 9, 48},
}

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
}

// Columns are the headings, in order, in English.
func Columns() []string { return ColumnsIn("") }

// ColumnsIn are the headings in one language.
//
// Exported because the interface shows the same names above its import preview,
// and two lists would drift.
func ColumnsIn(language string) []string { return headingsIn(language, timesheets) }

// dateFormat is how a date is written and read.
//
// ISO, and stored as text rather than as an Excel date. A real date cell carries a
// serial number whose meaning depends on the workbook's epoch setting, and a
// spreadsheet that has been through a machine with a different locale can come back
// with the day and the month exchanged - silently, and only for the days where both
// readings are valid. Text is unambiguous in every locale.
const dateFormat = "2006-01-02"

// Write builds the workbook in English.
func Write(rows []Row) ([]byte, error) { return WriteIn("", rows) }

// WriteIn builds the workbook with its headings in one language.
func WriteIn(language string, rows []Row) ([]byte, error) {
	cells := make([][]Cell, 0, len(rows))

	for _, row := range rows {
		cells = append(cells, []Cell{
			Text(row.Date.Format(dateFormat)),
			Text(row.User),
			Text(row.Project),
			// Hours as a number: it is what lets somebody total a column in Excel.
			Number(row.Hours),
			Text(row.Description),
		})
	}

	return write(timesheets, language, cells)
}

// Read parses a workbook back into rows.
//
// Every row it can read is returned, and every row it cannot is reported, in one
// pass: the caller decides whether a file with problems in it is worth writing, and
// cannot decide that from the first failure alone.
func Read(r io.Reader) ([]Row, []RowError, error) {
	raw, err := read(r, timesheets)
	if err != nil {
		return nil, nil, err
	}

	rows := make([]Row, 0, len(raw))
	problems := make([]RowError, 0)

	for i, cells := range raw {
		// The heading was row 1 and has been dropped, so the first data row is 2.
		number := i + 2

		row, rowErr := parseRow(number, cells)
		if rowErr != nil {
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

func parseRow(number int, cells []string) (Row, error) {
	value := cellReader(cells)

	if blank(value, len(timesheets.Headings)) {
		return Row{}, errBlankRow
	}

	parsedDate, err := parseDate(value(0))
	if err != nil {
		return Row{}, err
	}

	parsedHours, err := parseHours(value(3))
	if err != nil {
		return Row{}, err
	}

	return Row{
		Number:      number,
		Date:        parsedDate,
		User:        value(1),
		Project:     value(2),
		Hours:       parsedHours,
		Description: value(4),
	}, nil
}

// parseDate reads a date column.
//
// ISO first, because that is what this application writes. The other two forms are
// what Excel leaves behind when somebody types into the cell: the German order,
// and the serial number a cell formatted as a date turns into. Refusing those would
// mean refusing files that look perfectly correct on screen.
func parseDate(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, problemf("dateMissing", "the date is missing")
	}

	if parsed, ok := parseOptionalDate(raw); ok {
		return parsed, nil
	}

	return time.Time{}, problemf("dateNotUnderstood",
		"%q is not a date the importer understands (use YYYY-MM-DD)", raw)
}

// parseOptionalDate is parseDate for a column that may be empty, which returns
// the zero time and no complaint.
func parseOptionalDate(raw string) (time.Time, bool) {
	for _, layout := range []string{dateFormat, "02.01.2006", "01/02/2006", time.RFC3339} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return theDayItFallsOn(parsed), true
		}
	}

	// A serial number: days since the workbook's epoch. Converted by the library
	// so the epoch setting is honoured rather than assumed.
	if serial, err := strconv.ParseFloat(raw, 64); err == nil {
		if converted, convErr := excelize.ExcelDateToTime(serial, false); convErr == nil {
			return theDayItFallsOn(converted), true
		}
	}

	return time.Time{}, false
}

// theDayItFallsOn reduces what a cell yielded to the day it names.
//
// Two of the forms above carry more than a day. RFC 3339 brings whatever offset
// was written into the cell, and a serial number with a fractional part brings a
// time of day - a cell formatted as "date" in a workbook that was really holding
// a timestamp. Neither is wrong in the file; both are wrong in a column that
// answers which day somebody worked, where every other date is midnight UTC.
//
// The same rule as model.CalendarDay, written out rather than imported: this
// package reads and writes files and knows nothing about the domain, which is
// what lets it be tested against a workbook alone.
func theDayItFallsOn(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// parseHours reads an hours column, in either decimal convention.
//
// A comma is what a German keyboard produces and what German Excel writes, and
// reading "6,50" as nothing would reject a file that is plainly correct. Neither
// convention uses the other's separator as a thousands mark for a figure that has
// to be under 24, so accepting both is unambiguous here.
func parseHours(raw string) (float64, error) {
	if raw == "" {
		return 0, problemf("hoursMissing", "the hours are missing")
	}

	hours, err := strconv.ParseFloat(strings.ReplaceAll(raw, ",", "."), 64)
	if err != nil {
		return 0, problemf("hoursNotANumber", "%q is not a number of hours", raw)
	}

	return hours, nil
}
