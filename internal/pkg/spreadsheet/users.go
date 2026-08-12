package spreadsheet

import (
	"errors"
	"io"
	"strings"
)

// users is the sheet of people.
var users = Table{
	Key:      "Users",
	Headings: []string{"Name", "Email", "Role", "Directory"},
	Widths:   []float64{24, 30, 16, 12},
}

// UserRow is one account as the workbook holds it.
//
// No password column, in either direction. A spreadsheet of passwords is a
// spreadsheet that gets mailed around, and one this application generated would have
// to be read back out of the file to be of any use - so an import changes existing
// accounts rather than creating them, matched on the mail address. That makes it the
// right tool for what it is wanted for: moving a department to another role.
//
// No daily target, ceiling or time zone either, and they were here. They are time
// figures, and everything to do with time belongs to the person it is about, who sets
// it under My account - so a column for them would be one the import had to ignore,
// and a column that is silently ignored is worse than one that is missing: somebody
// edits forty of them and is told forty rows were written.
type UserRow struct {
	// Number is the row in the sheet, so a complaint can name where to look.
	Number int

	Name  string
	Email string
	Role  string

	// Directory says the password lives in LDAP. Written for information and ignored
	// on reading: an account cannot be moved into the directory, or out of it, by
	// editing a cell.
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

			problems = append(problems, rowErrorFor(number, rowErr))

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
		return UserRow{}, problemf("emailMissing", "the email address is missing, and it "+
			"is what the row is matched on")
	}

	// Checked for shape, not merely for being filled in. This column is the key
	// the row is matched on, so a cell holding something that is plainly not an
	// address is a sign the file is not what it was taken for - a column shifted
	// by an edit, or the wrong sheet entirely - and saying so names the row rather
	// than reporting forty accounts that do not exist.
	if !strings.Contains(email, "@") || strings.HasPrefix(email, "@") ||
		strings.HasSuffix(email, "@") {
		return UserRow{}, problemf("emailInvalid", "%q is not an email address, and this "+
			"column is what the row is matched on", value(1))
	}

	return UserRow{
		Number: number,
		Name:   value(0),
		Email:  email,
		Role:   value(2),
	}, nil
}
