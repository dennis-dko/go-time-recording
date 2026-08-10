package spreadsheet

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// users is the sheet of people.
var users = Table{
	Key: "Users",
	Headings: []string{
		"Name", "Email", "Role", "Daily target", "Daily maximum", "Time zone", "Directory",
	},
	Widths: []float64{24, 30, 16, 12, 14, 22, 12},
}

// UserRow is one account as the workbook holds it.
//
// No password column, in either direction. A spreadsheet of passwords is a
// spreadsheet that gets mailed around, and one this application generated would
// have to be read back out of the file to be of any use - so import changes
// existing accounts rather than creating them, matched on the mail address. That
// makes it the right tool for what it is actually wanted for: giving forty people
// a new daily target, or moving a department to another role.
type UserRow struct {
	// Number is the row in the sheet, so a complaint can name where to look.
	Number int

	Name  string
	Email string
	Role  string

	// DailyTargetHours and MaxDailyHours are zero for an account left on the
	// instance defaults, and stay on them when the cell is empty.
	DailyTargetHours float64
	MaxDailyHours    float64

	// Timezone is an IANA name, or empty for the instance setting.
	Timezone string

	// Directory says the password lives in LDAP. Written for information and
	// ignored on reading: an account cannot be moved into the directory, or out
	// of it, by editing a cell.
	Directory bool
}

// UserColumns are the people headings in one language.
func UserColumns(language string) []string { return headingsIn(language, users) }

// WriteUsers builds the workbook of people.
func WriteUsers(language string, rows []UserRow) ([]byte, error) {
	cells := make([][]Cell, 0, len(rows))

	for _, row := range rows {
		cells = append(cells, []Cell{
			Text(row.Name),
			Text(row.Email),
			Text(row.Role),
			Number(row.DailyTargetHours),
			Number(row.MaxDailyHours),
			Text(row.Timezone),
			Text(translate(language, yesNo(row.Directory))),
		})
	}

	return write(users, language, cells)
}

// ReadUsers parses a workbook of people.
func ReadUsers(r io.Reader) ([]UserRow, []RowError, error) {
	raw, err := read(r, users)
	if err != nil {
		return nil, nil, err
	}

	rows := make([]UserRow, 0, len(raw))
	problems := make([]RowError, 0)

	for i, cells := range raw {
		number := i + 2

		row, rowErr := parseUserRow(number, cells)
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

func parseUserRow(number int, cells []string) (UserRow, error) {
	value := cellReader(cells)

	if blank(value, len(users.Headings)) {
		return UserRow{}, errBlankRow
	}

	email := strings.ToLower(value(1))
	if email == "" {
		return UserRow{}, errors.New("the email address is missing, and it is what " +
			"the row is matched on")
	}

	// Checked for shape, not merely for being filled in. This column is the key
	// the row is matched on, so a cell holding something that is plainly not an
	// address is a sign the file is not what it was taken for - a column shifted
	// by an edit, or the wrong sheet entirely - and saying so names the row rather
	// than reporting forty accounts that do not exist.
	if !strings.Contains(email, "@") || strings.HasPrefix(email, "@") ||
		strings.HasSuffix(email, "@") {
		return UserRow{}, fmt.Errorf("%q is not an email address, and this column is "+
			"what the row is matched on", value(1))
	}

	target, err := parseOptionalHours(value(3), "daily target")
	if err != nil {
		return UserRow{}, err
	}

	maximum, err := parseOptionalHours(value(4), "daily maximum")
	if err != nil {
		return UserRow{}, err
	}

	return UserRow{
		Number:           number,
		Name:             value(0),
		Email:            email,
		Role:             value(2),
		DailyTargetHours: target,
		MaxDailyHours:    maximum,
		Timezone:         value(5),
	}, nil
}

// parseOptionalHours reads an hours column that may be left empty, which means
// "leave this as it is" rather than zero.
func parseOptionalHours(raw, column string) (float64, error) {
	if raw == "" {
		return 0, nil
	}

	hours, err := strconv.ParseFloat(strings.ReplaceAll(raw, ",", "."), 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a number of hours for the %s", raw, column)
	}

	return hours, nil
}
