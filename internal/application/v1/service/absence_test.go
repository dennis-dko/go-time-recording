package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// daysOf writes absences as their days, for comparing with a list.
func daysOf(absences []model.Absence) []string {
	days := make([]string, 0, len(absences))
	for _, absence := range absences {
		days = append(days, absence.Day.Format(time.DateOnly))
	}

	return days
}

// A fortnight of leave is the ten working days in it, not the fourteen the
// calendar counts - and read back, it is those ten.
func TestLeaveIsRecordedOnTheWorkingDaysOnly(t *testing.T) {
	f := newPlannedFixture(t)
	ctx := context.Background()

	recorded, err := f.away.Record(ctx, f.userID, day(13), day(26), model.AbsenceVacation)
	if err != nil {
		t.Fatalf("record: %v", err)
	}

	if len(recorded) != 10 {
		t.Errorf("a fortnight was recorded as %d days, want the ten working ones: %v",
			len(recorded), daysOf(recorded))
	}

	read, err := f.away.Absences(ctx, f.userID, day(13), day(26))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	for _, absence := range read {
		if weekday := absence.Day.Weekday(); weekday == time.Saturday || weekday == time.Sunday {
			t.Errorf("%s is recorded as leave and is a %s", absence.Day.Format(time.DateOnly), weekday)
		}
	}

	if len(read) != 10 {
		t.Errorf("read back %d days of leave, want 10", len(read))
	}
}

// The days somebody's own week leaves free are left out, whatever the calendar
// says about them: a four-day week takes four days of leave from a week of it.
func TestLeaveFollowsTheWeekThatWasPlanned(t *testing.T) {
	f := newPlannedFixture(t)

	f.plan(t, day(1), model.WeekHours{8, 8, 8, 8, 0, 0, 0})

	recorded, err := f.away.Record(context.Background(), f.userID, day(13), day(19), model.AbsenceVacation)
	if err != nil {
		t.Fatalf("record: %v", err)
	}

	if len(recorded) != 4 {
		t.Errorf("a week of a four-day week was recorded as %v, want Monday to Thursday", daysOf(recorded))
	}
}

// A day recorded again takes the new reason rather than becoming two.
func TestADayRecordedAgainTakesTheNewReason(t *testing.T) {
	f := newPlannedFixture(t)
	ctx := context.Background()

	for _, kind := range []string{model.AbsenceVacation, model.AbsenceSick} {
		if _, err := f.away.Record(ctx, f.userID, day(13), day(13), kind); err != nil {
			t.Fatalf("record %s: %v", kind, err)
		}
	}

	read, err := f.away.Absences(ctx, f.userID, day(13), day(13))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if len(read) != 1 || read[0].Kind != model.AbsenceSick {
		t.Errorf("the Monday reads %+v, want one absence, sick", read)
	}
}

// What cannot be recorded is refused by name: a weekend has no working day in
// it, a kind nobody knows is not one, and a range has to run forwards.
func TestAnAbsenceThatCannotBeRecordedIsRefused(t *testing.T) {
	f := newPlannedFixture(t)
	ctx := context.Background()

	_, err := f.away.Record(ctx, f.userID, day(18), day(19), model.AbsenceVacation)
	requireCode(t, err, "noWorkingDays")

	_, err = f.away.Record(ctx, f.userID, day(13), day(13), "holiday")
	requireKind(t, err, apperror.KindInvalid)

	_, err = f.away.Record(ctx, f.userID, day(14), day(13), model.AbsenceVacation)
	requireCode(t, err, "rangeInverted")
}

// An absence is taken back by its day, and a day with none is not there.
func TestAnAbsenceIsTakenBack(t *testing.T) {
	f := newPlannedFixture(t)
	ctx := context.Background()

	if _, err := f.away.Record(ctx, f.userID, day(13), day(14), model.AbsenceSick); err != nil {
		t.Fatalf("record: %v", err)
	}

	if err := f.away.Remove(ctx, f.userID, day(13)); err != nil {
		t.Fatalf("remove: %v", err)
	}

	requireKind(t, f.away.Remove(ctx, f.userID, day(13)), apperror.KindNotFound)

	if read, _ := f.away.Absences(ctx, f.userID, day(13), day(14)); len(read) != 1 {
		t.Errorf("%d absence(s) left, want the Tuesday's", len(read))
	}
}

// The days of a week say which of them somebody is away on.
func TestTheDaysSayWhoIsAway(t *testing.T) {
	f := newPlannedFixture(t)
	ctx := context.Background()

	if _, err := f.away.Record(ctx, f.userID, day(14), day(14), model.AbsenceHalfVacation); err != nil {
		t.Fatalf("record: %v", err)
	}

	days, err := f.overtime.Days(ctx, f.userID, day(13), day(19))
	if err != nil {
		t.Fatalf("days: %v", err)
	}

	for _, d := range days {
		want := ""
		if d.Date.Equal(day(14)) {
			want = model.AbsenceHalfVacation
		}

		if d.Absence != want {
			t.Errorf("%s reads %q, want %q", d.Date.Format(time.DateOnly), d.Absence, want)
		}
	}
}
