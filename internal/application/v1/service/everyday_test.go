package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// countFrom makes every working day count from a day on.
func (f *plannedFixture) countFrom(t *testing.T, from time.Time) {
	t.Helper()

	if _, err := f.users.UpdateWorkingTimes(context.Background(), command.UpdateWorkingTimesCommand{
		ID: f.userID, EveryDayCountsFrom: &from,
	}); err != nil {
		t.Fatalf("count every day from %s: %v", from.Format(time.DateOnly), err)
	}
}

// balanceOf is the balance between two days, failing the case where it cannot be
// worked out.
func (f *plannedFixture) balanceOf(t *testing.T, from, to time.Time) *service.OvertimeBalance {
	t.Helper()

	balance, err := f.overtime.Balance(context.Background(), f.userID, from, to)
	if err != nil {
		t.Fatalf("balance %s to %s: %v", from.Format(time.DateOnly), to.Format(time.DateOnly), err)
	}

	return balance
}

// From the day chosen, a working day nobody booked on counts as what it asks -
// the weekend asks nothing and is left out.
func TestFromTheChosenDayEveryWorkingDayCounts(t *testing.T) {
	f := newPlannedFixture(t)

	f.countFrom(t, day(13))
	f.book(t, day(13), 8)
	f.book(t, day(14), 6)

	balance := f.balanceOf(t, day(13), day(19))

	if len(balance.Days) != 5 || balance.TotalBalance != -26 {
		t.Errorf("a week of two booked days reads %d day(s) and %.2f, want the five working days and -26",
			len(balance.Days), balance.TotalBalance)
	}
}

// Before the day chosen nothing changes: only the days booked on count, and they
// ask what their week plans, holiday or not - the rule they were measured by.
func TestBeforeTheChosenDayOnlyTheBookedDaysCount(t *testing.T) {
	f := newPlannedFixture(t)

	f.countFrom(t, day(15))

	balance := f.balanceOf(t, day(13), day(17))

	if balance.TotalBalance != -24 {
		t.Errorf("Monday to Friday counted from the Wednesday reads %.2f, want three days of eight hours short",
			balance.TotalBalance)
	}

	for _, d := range balance.Days {
		if d.Date.Before(day(15)) {
			t.Errorf("%s counts, and is before the day every day counts from", d.Date.Format(time.DateOnly))
		}
	}

	days, err := f.overtime.Days(context.Background(), f.userID, day(13), day(14))
	if err != nil {
		t.Fatalf("days: %v", err)
	}

	for _, d := range days {
		if d.Counts {
			t.Errorf("%s says it counts, unbooked and before the day every day counts from",
				d.Date.Format(time.DateOnly))
		}
	}

	// Whit Monday, booked before the day: measured as it was, against the eight
	// hours its week plans, rather than as a holiday that asks nothing.
	f.region = "BY"
	f.countFrom(t, on(time.May, 26))
	f.book(t, on(time.May, 25), 2)

	if whit := f.balanceOf(t, on(time.May, 25), on(time.May, 25)); whit.TotalBalance != -6 {
		t.Errorf("Whit Monday booked before the day reads %.2f, want -6 as it always did", whit.TotalBalance)
	}
}

// What a holiday and each kind of absence take off a day: Whit Monday asks
// nothing and counts as overtime what was booked on it, leave and illness ask
// nothing, half a day of leave asks half, and a day off against overtime asks the
// whole day - which is what takes it off the balance.
func TestHolidaysAndAbsencesTakeOffWhatADayAsks(t *testing.T) {
	f := newPlannedFixture(t)
	f.region = "BY"
	ctx := context.Background()

	f.countFrom(t, on(time.May, 25))

	for d, kind := range map[int]string{
		26: model.AbsenceVacation, 27: model.AbsenceHalfVacation, 28: model.AbsenceSick, 29: model.AbsenceFlexDay,
	} {
		if _, err := f.away.Record(ctx, f.userID, on(time.May, d), on(time.May, d), kind); err != nil {
			t.Fatalf("record %s: %v", kind, err)
		}
	}

	f.book(t, on(time.May, 25), 2) // on Whit Monday
	f.book(t, on(time.May, 27), 4) // the half day of leave, worked

	balance := f.balanceOf(t, on(time.May, 25), on(time.May, 29))

	counted := map[string]service.OvertimeDay{}
	for _, d := range balance.Days {
		counted[d.Date.Format(time.DateOnly)] = d
	}

	for date, want := range map[string]float64{"2026-05-25": 2, "2026-05-27": 0, "2026-05-29": -8} {
		if got, ok := counted[date]; !ok || got.Balance != want {
			t.Errorf("%s reads %+v, want it counted at %.2f", date, got, want)
		}
	}

	for _, date := range []string{"2026-05-26", "2026-05-28"} {
		if _, ok := counted[date]; ok {
			t.Errorf("%s counts, and leave and illness ask nothing", date)
		}
	}

	if balance.TotalBalance != -6 {
		t.Errorf("the week reads %.2f, want -6", balance.TotalBalance)
	}

	if counted["2026-05-29"].Absence != model.AbsenceFlexDay || counted["2026-05-25"].Holiday != "whitMonday" {
		t.Error("the days that ask less do not say why")
	}
}

// A working day not yet had does not count until something is booked on it.
func TestADayNotYetHadCountsOnlyOnceBooked(t *testing.T) {
	f := newPlannedFixture(t)
	f.today = day(15)

	f.countFrom(t, day(13))
	f.book(t, day(17), 2) // the Friday, booked ahead

	balance := f.balanceOf(t, day(13), day(17))

	if len(balance.Days) != 4 || balance.TotalBalance != -30 {
		t.Errorf("a week seen on its Wednesday reads %d day(s) and %.2f, want Monday to Wednesday "+
			"and the booked Friday, -30", len(balance.Days), balance.TotalBalance)
	}
}

// The days of a week say which of them count, by the rule the balance keeps, so
// a picture adding them up comes to the balance's figure.
func TestTheDaysSayWhichOfThemCount(t *testing.T) {
	f := newPlannedFixture(t)
	f.today = day(15)

	f.countFrom(t, day(13))

	days, err := f.overtime.Days(context.Background(), f.userID, day(13), day(19))
	if err != nil {
		t.Fatalf("days: %v", err)
	}

	for _, d := range days {
		want := !d.Date.After(day(15))

		if d.Counts != want {
			t.Errorf("%s counts: %v, want %v", d.Date.Format(time.DateOnly), d.Counts, want)
		}
	}
}

// Taking the day back puts the old rule back: only the days booked on count.
func TestTakingTheDayBackCountsOnlyTheBookedDaysAgain(t *testing.T) {
	f := newPlannedFixture(t)

	f.countFrom(t, day(13))
	f.book(t, day(13), 8)

	if _, err := f.users.UpdateWorkingTimes(context.Background(), command.UpdateWorkingTimesCommand{
		ID: f.userID, ClearEveryDayCountsFrom: true,
	}); err != nil {
		t.Fatalf("take the day back: %v", err)
	}

	if balance := f.balanceOf(t, day(13), day(17)); len(balance.Days) != 1 || balance.TotalBalance != 0 {
		t.Errorf("without a day the week reads %d day(s) and %.2f, want the one booked day at 0",
			len(balance.Days), balance.TotalBalance)
	}
}
