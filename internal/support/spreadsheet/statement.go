package spreadsheet

// statement is the sheet a statement of work is written to. Only written, never
// read: it is a figure for somebody's books, and nothing here imports one.
var statement = Table{
	Key:      "Statement of work",
	Headings: []string{"Project", "Hours", "Rate", "Currency", "Amount"},
	Widths:   []float64{36, 10, 10, 10, 14},
}

// StatementRow is one line of a statement: a project, or with Total set what the
// lines in one currency come to. Rate and Amount are in whole units of Currency,
// nil where there is no rate - an empty cell, not a nought somebody might add up.
type StatementRow struct {
	Project  string
	Total    bool
	Hours    float64
	Rate     *float64
	Currency string
	Amount   *float64
}

// WriteStatement builds the workbook of a statement of work.
func WriteStatement(language string, rows []StatementRow) ([]byte, error) {
	cells := make([][]Cell, 0, len(rows))

	for _, row := range rows {
		// A total is named in the reader's language, and a project by its own
		// name, which is never a word to translate whatever it happens to say.
		label := row.Project
		if row.Total {
			label = translate(language, "Total")
		}

		line := []Cell{Text(label), Number(row.Hours), Text(""), Text(row.Currency), Text("")}

		if row.Rate != nil {
			line[2] = Number(*row.Rate)
		}

		if row.Amount != nil {
			line[4] = Number(*row.Amount)
		}

		cells = append(cells, line)
	}

	return write(statement, language, cells)
}
