package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// entitle gives somebody days of leave a year from a year on.
func (f *plannedFixture) entitle(t *testing.T, fromYear int, days float64) {
	t.Helper()

	if err := f.leave.Entitle(context.Background(), f.userID, fromYear, days); err != nil {
		t.Fatalf("entitle %.1f days from %d: %v", days, fromYear, err)
	}
}

// takeLeave records leave from one day to another.
func (f *plannedFixture) takeLeave(t *testing.T, from, to time.Time, kind string) {
	t.Helper()

	if _, err := f.away.Record(context.Background(), f.userID, from, to, kind); err != nil {
		t.Fatalf("record %s from %s: %v", kind, from.Format(time.DateOnly), err)
	}
}

// leaveIn is somebody's leave in a year.
func (f *plannedFixture) leaveIn(t *testing.T, year int) *service.VacationAccount {
	t.Helper()

	account, err := f.leave.Year(context.Background(), f.userID, year)
	if err != nil {
		t.Fatalf("leave in %d: %v", year, err)
	}

	return account
}

// A year's leave is what it gives less what is taken - up to today - and what is
// planned after it, a half day counting a half.
func TestLeaveIsCountedAgainstTheYear(t *testing.T) {
	f := newPlannedFixture(t) // today is 31 July 2026

	f.entitle(t, 2026, 30)
	f.takeLeave(t, day(13), day(17), model.AbsenceVacation)
	f.takeLeave(t, day(20), day(20), model.AbsenceHalfVacation)
	f.takeLeave(t, on(time.August, 3), on(time.August, 7), model.AbsenceVacation)
	f.takeLeave(t, day(21), day(21), model.AbsenceSick)

	account := f.leaveIn(t, 2026)

	if account.Entitlement != 30 || account.Taken != 5.5 || account.Planned != 5 || account.Remaining() != 19.5 {
		t.Errorf("2026 reads %+v, want 30 given, 5.5 taken, 5 planned and 19.5 left", account.VacationYear)
	}
}

// What a year leaves over is carried into the next - and what it overdraws is
// owed - and a later change to the allowance leaves the years before it alone.
func TestWhatAYearLeavesIsCarriedIntoTheNext(t *testing.T) {
	f := newPlannedFixture(t)

	f.entitle(t, 2025, 28)
	f.takeLeave(t, time.Date(2025, time.December, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2025, time.December, 5, 0, 0, 0, 0, time.UTC), model.AbsenceVacation)

	if account := f.leaveIn(t, 2026); account.CarriedOver != 23 || account.Entitlement != 28 {
		t.Errorf("2026 reads %+v, want 28 given and the 23 left in 2025 carried", account.VacationYear)
	}

	f.entitle(t, 2026, 30)

	if account := f.leaveIn(t, 2026); account.CarriedOver != 23 || account.Entitlement != 30 {
		t.Errorf("with 30 from 2026, 2026 reads %+v, want 30 given and still 23 carried", account.VacationYear)
	}

	f.entitle(t, 2025, 2)

	if account := f.leaveIn(t, 2026); account.CarriedOver != -3 {
		t.Errorf("five days taken of two leave %.1f carried, want the three overdrawn owed", account.CarriedOver)
	}
}

// Leave recorded on a day that has since become one not worked - a state chosen
// later whose holiday it is - is not counted against the year.
func TestLeaveOnADayNotWorkedIsNotCounted(t *testing.T) {
	f := newPlannedFixture(t)

	f.entitle(t, 2026, 30)
	f.takeLeave(t, on(time.May, 25), on(time.May, 26), model.AbsenceVacation)

	if account := f.leaveIn(t, 2026); account.Taken != 2 {
		t.Fatalf("two days of leave read %.1f taken", account.Taken)
	}

	f.region = "BY"

	if account := f.leaveIn(t, 2026); account.Taken != 1 {
		t.Errorf("with Whit Monday a holiday, %.1f days are taken, want the Tuesday alone", account.Taken)
	}
}

// There is no account before the first year anything was given, and what cannot
// be given is refused.
func TestLeaveStartsWithTheFirstEntitlement(t *testing.T) {
	f := newPlannedFixture(t)
	ctx := context.Background()

	f.entitle(t, 2026, 30)

	if account := f.leaveIn(t, 2025); account.Entitlement != 0 || account.Remaining() != 0 || len(account.Entitlements) != 1 {
		t.Errorf("the year before the first entitlement reads %+v", account)
	}

	for _, refused := range []error{
		f.leave.Entitle(ctx, f.userID, 1800, 30),
		f.leave.Entitle(ctx, f.userID, 2027, -1),
		f.leave.Entitle(ctx, f.userID, 2027, 400),
	} {
		requireKind(t, refused, apperror.KindInvalid)
	}

	requireKind(t, f.leave.Withdraw(ctx, f.userID, 2030), apperror.KindNotFound)
}
