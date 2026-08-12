package spreadsheet

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

// roles is the fixed part of the sheet of roles: what it is called, and the three
// columns every role has whatever rights the application enforces.
//
// The rest of the columns are the permissions themselves, one each, and they are
// not listed here because this package does not know them - the application does,
// and it passes them in. Keeping the Key here is still worth it: it is what lets a
// workbook of the wrong kind be recognised by its sheet name.
var roles = Table{
	Key:      "Roles",
	Headings: []string{"Name", "Description", "System"},
	Widths:   []float64{24, 40, 10},
}

// RoleRow is one role as the workbook holds it.
//
// A column per permission holding yes or no, rather than one cell listing them.
// A list in a cell reads well and imports badly: a typo in it is a right silently
// dropped, and nothing about "roles:read, projects:wrote" looks wrong until
// somebody cannot open a screen. A column is a question with two answers, and an
// unrecognised heading can be reported by name.
type RoleRow struct {
	// Number is the row in the sheet, so a complaint can name where to look.
	Number int

	Name        string
	Description string

	// Granted is the permissions this row ticks, in no particular order.
	Granted []string

	// System says the application depends on this role. Written for information and
	// ignored on reading: a role cannot be made system, or stop being one, by
	// editing a cell.
	System bool
}

// RoleColumns are the role headings in one language, for the given permissions.
//
// The permission columns are not translated, and that is deliberate: a permission
// is an identifier that the role editor shows verbatim, and a heading that read
// "Projekte lesen" in one export and "projects:read" in another would be two names
// for one column - with only one of them matching what is on screen.
func RoleColumns(language string, permissions []string) []string {
	return append(headingsIn(language, roles), permissions...)
}

// WriteRoles builds the workbook of roles.
func WriteRoles(language string, permissions []string, rows []RoleRow) ([]byte, error) {
	table := roles
	table.Headings = append(append([]string{}, roles.Headings...), permissions...)
	table.Widths = append(append([]float64{}, roles.Widths...), widthsFor(permissions)...)

	cells := make([][]Cell, 0, len(rows))

	for _, row := range rows {
		held := make(map[string]bool, len(row.Granted))
		for _, permission := range row.Granted {
			held[permission] = true
		}

		line := []Cell{
			Text(row.Name),
			Text(row.Description),
			Text(translate(language, yesNo(row.System))),
		}

		for _, permission := range permissions {
			line = append(line, Text(translate(language, yesNo(held[permission]))))
		}

		cells = append(cells, line)
	}

	return write(table, language, cells)
}

// widthsFor opens each permission column wide enough for its name.
func widthsFor(permissions []string) []float64 {
	out := make([]float64, 0, len(permissions))

	for _, permission := range permissions {
		// A little past the heading, which is the longest thing in the column: the
		// cells below it hold yes or no.
		out = append(out, float64(len(permission))+4)
	}

	return out
}

// ReadRoles parses a workbook of roles.
//
// The permission columns are matched by heading rather than by position, which is
// the whole reason this reader is not like the others. The columns are the rights
// the application enforces, and that list grows - so a file exported before a right
// existed is one column short, and reading it by position would tick a different
// right on every role in it. A heading nobody recognises is reported rather than
// ignored, because an unrecognised right is either a typo or a file from another
// installation, and both are worth stopping for.
func ReadRoles(r io.Reader, permissions []string) ([]RoleRow, []RowError, error) {
	heading, raw, err := readWithHeading(r, roles)
	if err != nil {
		return nil, nil, err
	}

	columns, err := permissionColumns(heading, permissions)
	if err != nil {
		return nil, nil, err
	}

	rows := make([]RoleRow, 0, len(raw))
	problems := make([]RowError, 0)

	for i, cells := range raw {
		number := i + 2

		row, rowErr := parseRoleRow(number, cells, columns)
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

// UnknownColumnError is a heading naming a right this application does not
// enforce.
//
// Its own type rather than a formatted error, so the column can be named in the
// reader's language: this is the one complaint the column-per-right arrangement
// exists to be able to make, and answering it with "this is not a readable .xlsx
// workbook" - which is what a plain error was turned into - throws away both the
// name and the truth, since the file reads perfectly well.
type UnknownColumnError struct{ Name string }

func (e UnknownColumnError) Error() string {
	return fmt.Sprintf("%q is not a permission this application enforces; "+
		"the file may come from another installation", e.Name)
}

// permissionColumns maps each permission to the column it was found in.
//
// A file may leave rights out - a column somebody deleted is a right this import
// simply does not touch - but it may not carry one nobody has heard of.
func permissionColumns(heading, permissions []string) (map[string]int, error) {
	known := make(map[string]bool, len(permissions))
	for _, permission := range permissions {
		known[permission] = true
	}

	columns := make(map[string]int, len(permissions))

	for index, cell := range heading {
		name := strings.TrimSpace(cell)

		// The first columns are the role itself, in whichever language the file was
		// written, so they are skipped by position rather than by name.
		if index < len(roles.Headings) {
			continue
		}

		if name == "" {
			continue
		}

		if !known[name] {
			return nil, UnknownColumnError{Name: name}
		}

		columns[name] = index
	}

	return columns, nil
}

func parseRoleRow(number int, cells []string, columns map[string]int) (RoleRow, error) {
	value := cellReader(cells)

	if blank(value, len(cells)) {
		return RoleRow{}, errBlankRow
	}

	name := strings.TrimSpace(value(0))
	if name == "" {
		return RoleRow{}, problemf("roleNameMissing", "the name is missing, and it is what "+
			"the row is matched on")
	}

	row := RoleRow{Number: number, Name: name, Description: value(1)}

	for permission, index := range columns {
		if ticked(value(index)) {
			row.Granted = append(row.Granted, permission)
		}
	}

	return row, nil
}

// ticked reads a yes/no cell in either language, and tolerates the spellings
// people actually type.
//
// Anything unrecognised is a no. The alternative is refusing the row over a cell
// somebody wrote "Y" in, and a right that is not granted is the safe half of the
// two - a role that grants less than intended is noticed by whoever holds it, one
// that grants more is not noticed at all.
func ticked(cell string) bool {
	switch strings.ToLower(strings.TrimSpace(cell)) {
	case "yes", "ja", "y", "j", "true", "wahr", "x", "1":
		return true
	default:
		return false
	}
}
