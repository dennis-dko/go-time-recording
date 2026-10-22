package service_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// billableProject makes a project of the fixture's account with what its hours are
// charged at.
func (f *fixture) billableProject(t *testing.T, name string, billable bool, rate *float64,
	currency string) uint {
	t.Helper()

	created, err := f.projects.CreateProject(context.Background(), command.CreateProjectCommand{
		Name: name, StartDate: day(1), OwnerID: &f.userID,
		Billable: billable, HourlyRate: rate, Currency: currency,
	})
	if err != nil {
		t.Fatalf("make %s: %v", name, err)
	}

	return created.Result.ID
}

// bookOn books hours of the fixture's account on a project.
func (f *fixture) bookOn(t *testing.T, projectID uint, on time.Time, hours float64) {
	t.Helper()

	if _, err := f.timesheets.CreateTimesheet(context.Background(), command.CreateTimesheetCommand{
		UserID: f.userID, ProjectID: projectID, Date: on, DurationHours: hours,
	}); err != nil {
		t.Fatalf("book %.2fh: %v", hours, err)
	}
}

// A statement lists the billable projects of the period, a line each with what
// their hours come to at their rate, and a total per currency - never one total
// across two currencies, nothing for a project that is not billable, and no
// amount guessed for one that has no rate yet.
func TestAStatementChargesTheBillableHoursPerProjectAndCurrency(t *testing.T) {
	f := newFixture(t)

	consulting := f.billableProject(t, "Consulting", true, ptr(85.5), "eur")
	support := f.billableProject(t, "Support", true, nil, "")
	zurich := f.billableProject(t, "Zurich", true, ptr(120.0), "CHF")
	internal := f.billableProject(t, "Internal", false, ptr(50.0), "")

	// 2.5 + 0.1 + 0.2 is 2.8000000000000003 in a double, which is what the
	// statement has to be forgiving about.
	f.bookOn(t, consulting, day(6), 2.5)
	f.bookOn(t, consulting, day(7), 0.1)
	f.bookOn(t, consulting, day(8), 0.2)
	f.bookOn(t, support, day(6), 1)
	f.bookOn(t, zurich, day(7), 3)
	f.bookOn(t, internal, day(8), 4)
	f.book(t, day(9), 1)

	// Outside the period, so not in it.
	f.bookOn(t, consulting, time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC), 7)

	statement, err := service.NewStatementService(f.timesheetRepo, f.projectRepo).
		Statement(context.Background(), f.userID, day(1), day(31))
	if err != nil {
		t.Fatalf("statement: %v", err)
	}

	type line struct {
		name     string
		hours    float64
		currency string
		rate     int64
		amount   int64
		priced   bool
	}

	var got []line

	for _, l := range statement.Lines {
		read := line{name: l.Name, hours: l.Hours, currency: l.Currency}

		if l.HourlyRate != nil && l.Amount != nil {
			read.rate, read.amount, read.priced = *l.HourlyRate, *l.Amount, true
		}

		got = append(got, read)
	}

	want := []line{
		{name: "Consulting", hours: 2.8, currency: "EUR", rate: 8550, amount: 23940, priced: true},
		{name: "Support", hours: 1, currency: "EUR"},
		{name: "Zurich", hours: 3, currency: "CHF", rate: 12000, amount: 36000, priced: true},
	}

	if len(got) != len(want) {
		t.Fatalf("the statement has the lines %+v, want %+v", got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d reads %+v, want %+v", i, got[i], want[i])
		}
	}

	if len(statement.Totals) != 2 ||
		statement.Totals[0] != (service.StatementTotal{Currency: "CHF", Hours: 3, Amount: 36000}) ||
		statement.Totals[1] != (service.StatementTotal{Currency: "EUR", Hours: 2.8, Amount: 23940}) {
		t.Errorf("the totals read %+v, want 360.00 CHF for 3 h and 239.40 EUR for 2.8 h", statement.Totals)
	}
}

// A rate is kept to the hundredth and its currency in capitals, zero takes the rate
// off again, and a rate below nothing or beyond any that is charged, or a currency
// that is not three letters, is refused by name.
func TestARateAndItsCurrencyAreKeptOrRefusedByName(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	id := f.billableProject(t, "Consulting", true, ptr(85.555), "chf")

	read := func() *model.Project {
		project, err := f.projectRepo.GetByID(ctx, id)
		if err != nil {
			t.Fatalf("read the project back: %v", err)
		}

		return project
	}

	if project := read(); project.HourlyRate == nil || *project.HourlyRate != 8556 || project.Currency != "CHF" {
		kept := "no rate"
		if project.HourlyRate != nil {
			kept = fmt.Sprintf("%d hundredths", *project.HourlyRate)
		}

		t.Errorf("the project keeps %s in %q, want 8556 hundredths in CHF", kept, project.Currency)
	}

	if _, err := f.projects.UpdateProject(ctx, command.UpdateProjectCommand{
		ID: id, ActorID: f.userID, HourlyRate: ptr(0.0),
	}); err != nil {
		t.Fatalf("take the rate off: %v", err)
	}

	if project := read(); project.HourlyRate != nil {
		t.Errorf("a rate of nothing left %d hundredths, want no rate", *project.HourlyRate)
	}

	for _, refused := range []struct {
		field  string
		change command.UpdateProjectCommand
	}{
		{"hourlyRate", command.UpdateProjectCommand{HourlyRate: ptr(-1.0)}},
		{"hourlyRate", command.UpdateProjectCommand{HourlyRate: ptr(model.MaxHourlyRate + 1.0)}},
		{"currency", command.UpdateProjectCommand{Currency: ptr("EURO")}},
		{"currency", command.UpdateProjectCommand{Currency: ptr("E1R")}},
	} {
		refused.change.ID, refused.change.ActorID = id, f.userID

		_, err := f.projects.UpdateProject(ctx, refused.change)

		detail, ok := apperror.Detail(err)
		if !ok || detail.Kind != apperror.KindInvalid || len(detail.Fields) != 1 || detail.Fields[0] != refused.field {
			t.Errorf("%+v answered %v, want %s refused as invalid", refused.change, err, refused.field)
		}
	}
}
