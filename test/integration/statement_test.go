//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/xuri/excelize/v2"
)

// pricedProject is a project as the API answers with what its hours are charged at.
type pricedProject struct {
	ID         uint     `json:"id"`
	Billable   bool     `json:"billable"`
	HourlyRate *float64 `json:"hourlyRate"`
	Currency   string   `json:"currency"`
}

// A statement of work lists somebody's billable projects of a period with their
// hours, rate and amount, totalled per currency - and the workbook says the same in
// the reader's language.
func TestAStatementOfWorkChargesTheBillableHoursOfAPeriod(t *testing.T) {
	t.Parallel()

	_, _, worker := startWithWorker(t)

	create := func(body map[string]any) pricedProject {
		var made pricedProject

		worker.must(worker.api(http.MethodPost, "/projects", body), http.StatusCreated, http.StatusOK).
			Data(t, &made)

		return made
	}

	consulting := create(map[string]any{
		"name": "Consulting", "startDate": "2026-10-01", "billable": true, "hourlyRate": 85.5,
	})
	zurich := create(map[string]any{
		"name": "Zurich", "startDate": "2026-10-01", "billable": true, "hourlyRate": 120, "currency": "chf",
	})
	internal := create(map[string]any{"name": "Internal", "startDate": "2026-10-01", "hourlyRate": 50})

	if !consulting.Billable || consulting.HourlyRate == nil || *consulting.HourlyRate != 85.5 ||
		consulting.Currency != "EUR" || zurich.Currency != "CHF" || internal.Billable {
		t.Fatalf("the projects read %+v, %+v and %+v", consulting, zurich, internal)
	}

	for _, entry := range []map[string]any{
		{"projectId": consulting.ID, "date": "2026-10-05", "durationHours": 2.5},
		{"projectId": consulting.ID, "date": "2026-10-06", "durationHours": 0.25},
		{"projectId": zurich.ID, "date": "2026-10-06", "durationHours": 3},
		{"projectId": internal.ID, "date": "2026-10-07", "durationHours": 4},
	} {
		entryOf(t, worker, entry)
	}

	var statement struct {
		Lines []struct {
			Name       string   `json:"name"`
			Hours      float64  `json:"hours"`
			Currency   string   `json:"currency"`
			HourlyRate *float64 `json:"hourlyRate"`
			Amount     *float64 `json:"amount"`
		} `json:"lines"`
		Totals []struct {
			Currency string  `json:"currency"`
			Hours    float64 `json:"hours"`
			Amount   float64 `json:"amount"`
		} `json:"totals"`
	}

	const period = "?from=2026-10-01&to=2026-10-31"

	worker.must(worker.api(http.MethodGet, "/reports/statement"+period, nil), http.StatusOK).
		Data(t, &statement)

	if len(statement.Lines) != 2 || statement.Lines[0].Name != "Consulting" ||
		statement.Lines[0].Hours != 2.75 || statement.Lines[0].Amount == nil ||
		*statement.Lines[0].Amount != 235.13 || statement.Lines[1].Name != "Zurich" ||
		statement.Lines[1].Amount == nil || *statement.Lines[1].Amount != 360 {
		raw, _ := json.Marshal(statement.Lines)
		t.Errorf("the lines read %s, want Consulting at 235.13 EUR and Zurich at 360 CHF", raw)
	}

	if len(statement.Totals) != 2 || statement.Totals[0].Currency != "CHF" ||
		statement.Totals[1].Currency != "EUR" || statement.Totals[1].Amount != 235.13 {
		raw, _ := json.Marshal(statement.Totals)
		t.Errorf("the totals read %s, want one for CHF and one for EUR", raw)
	}

	workbook := worker.must(worker.api(http.MethodGet, "/reports/statement/export"+period+"&lang=de", nil),
		http.StatusOK)

	book, err := excelize.OpenReader(bytes.NewReader(workbook.Body))
	if err != nil {
		t.Fatalf("opening the workbook: %v", err)
	}

	defer func() { _ = book.Close() }()

	rows, err := book.GetRows("Leistungsnachweis")
	if err != nil {
		t.Fatalf("reading the German sheet: %v (sheets %v)", err, book.GetSheetList())
	}

	labels := make([]string, 0, len(rows))
	for _, row := range rows {
		labels = append(labels, row[0])
	}

	if !slices.Equal(labels, []string{"Projekt", "Consulting", "Zurich", "Summe", "Summe"}) {
		t.Errorf("the sheet's first column reads %v, want the heading, both projects and two totals", labels)
	}
}

// A rate below nothing or beyond any that is charged, and a currency that is not
// three letters, are refused by the field's name.
func TestARateOrCurrencyThatCannotBeChargedIsRefusedByName(t *testing.T) {
	t.Parallel()

	_, _, worker := startWithWorker(t)

	var project pricedProject

	worker.must(worker.api(http.MethodPost, "/projects", map[string]any{
		"name": "Consulting", "startDate": "2026-10-01",
	}), http.StatusCreated, http.StatusOK).Data(t, &project)

	for _, refused := range []struct {
		change map[string]any
		field  string
	}{
		{map[string]any{"hourlyRate": -1}, "hourlyRate"},
		{map[string]any{"hourlyRate": 100001}, "hourlyRate"},
		{map[string]any{"currency": "EURO"}, "currency"},
	} {
		answer := worker.api(http.MethodPut, path("/projects/", project.ID), refused.change)

		var body struct {
			Error struct {
				Param []string `json:"param"`
			} `json:"error"`
		}

		_ = json.Unmarshal(answer.Body, &body)

		if answer.Status != http.StatusBadRequest || !slices.Equal(body.Error.Param, []string{refused.field}) {
			t.Errorf("%v answered %d %s, want 400 naming %s", refused.change, answer.Status, answer.Body,
				refused.field)
		}
	}
}
