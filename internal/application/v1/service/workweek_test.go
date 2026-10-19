package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/memory"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// July 2026, which the fixture's dates are in: the 13th and the 20th are
// Mondays, the 17th a Friday, the 18th and 19th a weekend.

// plannedFixture is the booking fixture with the weekly plans beside it.
type plannedFixture struct {
	*fixture

	weeks    *service.WorkScheduleService
	overtime *service.OvertimeService
}

func newPlannedFixture(t *testing.T) *plannedFixture {
	t.Helper()

	f := newFixture(t)
	schedules := memory.NewWorkScheduleRepository()

	return &plannedFixture{
		fixture:  f,
		weeks:    service.NewWorkScheduleService(schedules, f.userRepo),
		overtime: service.NewOvertimeService(f.timesheetRepo, f.userRepo, schedules),
	}
}

func (f *plannedFixture) plan(t *testing.T, from time.Time, hours model.WeekHours) {
	t.Helper()

	if _, err := f.weeks.Plan(context.Background(), f.userID, from, hours); err != nil {
		t.Fatalf("plan a week from %s: %v", from.Format(time.DateOnly), err)
	}
}

// balanceOn is the balance of one day in the period, and whether it counted.
func balanceOn(balance *service.OvertimeBalance, on time.Time) (float64, bool) {
	for _, d := range balance.Days {
		if d.Date.Equal(on) {
			return d.Balance, true
		}
	}

	return 0, false
}

// The finding the weekly plan began with. Every booked day was measured against
// the full daily target whichever day it was, so four hours worked on a Saturday
// stood in the balance as four hours short. Without a plan of their own somebody's
// week is their target from Monday to Friday and nothing at the weekend, so the
// same Saturday is four hours over.
func TestWorkAtTheWeekendIsOvertimeWithoutAPlan(t *testing.T) {
	f := newPlannedFixture(t)

	f.book(t, day(18), 4) // a Saturday
	f.book(t, day(20), 8) // a Monday

	balance, err := f.overtime.Balance(context.Background(), f.userID, day(13), day(26))
	if err != nil {
		t.Fatalf("balance: %v", err)
	}

	if saturday, _ := balanceOn(balance, day(18)); saturday != 4 {
		t.Errorf("four hours on a Saturday stand as %+.2f, want +4", saturday)
	}

	if balance.TotalBalance != 4 {
		t.Errorf("the period stands at %+.2f, want +4", balance.TotalBalance)
	}
}

// A plan says what each weekday asks: a six-hour Friday booked for six hours is
// a balanced day, not two hours short of somebody else's eight.
func TestAWeeklyPlanSetsTheTargetOfEachDay(t *testing.T) {
	f := newPlannedFixture(t)

	f.plan(t, day(13), model.WeekHours{8, 8, 8, 8, 6, 0, 0})

	f.book(t, day(16), 8) // Thursday
	f.book(t, day(17), 6) // Friday

	balance, err := f.overtime.Balance(context.Background(), f.userID, day(13), day(19))
	if err != nil {
		t.Fatalf("balance: %v", err)
	}

	if friday, _ := balanceOn(balance, day(17)); friday != 0 {
		t.Errorf("a six-hour Friday booked for six stands at %+.2f, want 0", friday)
	}

	if balance.TotalTarget != 14 || balance.TotalBalance != 0 {
		t.Errorf("target %.2f, balance %+.2f; want 14 and 0", balance.TotalTarget, balance.TotalBalance)
	}
}

// A plan starts on its day and leaves the weeks before it to the week that was
// planned then, so going part time does not rewrite last month.
func TestAPlanLeavesTheDaysBeforeItAlone(t *testing.T) {
	f := newPlannedFixture(t)

	f.plan(t, day(20), model.WeekHours{8, 8, 8, 8, 6, 0, 0})

	f.book(t, day(17), 6) // the Friday before the plan: the default eight
	f.book(t, day(24), 6) // a Friday in it: six

	balance, err := f.overtime.Balance(context.Background(), f.userID, day(13), day(26))
	if err != nil {
		t.Fatalf("balance: %v", err)
	}

	if before, _ := balanceOn(balance, day(17)); before != -2 {
		t.Errorf("the Friday before the plan stands at %+.2f, want -2", before)
	}

	if after, _ := balanceOn(balance, day(24)); after != 0 {
		t.Errorf("the Friday under the plan stands at %+.2f, want 0", after)
	}
}

// A later plan takes over from its own day, and the earlier one keeps the days
// before it.
func TestALaterPlanTakesOverFromItsDay(t *testing.T) {
	f := newPlannedFixture(t)

	f.plan(t, day(6), model.WeekHours{8, 8, 8, 8, 8, 0, 0})
	f.plan(t, day(20), model.WeekHours{4, 8, 8, 8, 8, 0, 0})

	f.book(t, day(13), 4) // a Monday under the first plan
	f.book(t, day(20), 4) // a Monday under the second

	balance, err := f.overtime.Balance(context.Background(), f.userID, day(6), day(26))
	if err != nil {
		t.Fatalf("balance: %v", err)
	}

	if first, _ := balanceOn(balance, day(13)); first != -4 {
		t.Errorf("the Monday under the first plan stands at %+.2f, want -4", first)
	}

	if second, _ := balanceOn(balance, day(20)); second != 0 {
		t.Errorf("the Monday under the second plan stands at %+.2f, want 0", second)
	}
}

// The plans come back in the order they apply, with the week that applies before
// the first - which is what a screen showing them needs to say about the days
// nobody planned.
func TestThePlansAreReadBackInTheOrderTheyApply(t *testing.T) {
	f := newPlannedFixture(t)
	ctx := context.Background()

	target := 7.5
	if _, err := f.users.UpdateWorkingTimes(ctx,
		command.UpdateWorkingTimesCommand{ID: f.userID, DailyTargetHours: &target}); err != nil {
		t.Fatalf("set the daily target: %v", err)
	}

	f.plan(t, day(20), model.WeekHours{6, 6, 6, 6, 6, 0, 0})
	f.plan(t, day(6), model.WeekHours{8, 8, 8, 8, 8, 0, 0})

	planned, err := f.weeks.Planned(ctx, f.userID)
	if err != nil {
		t.Fatalf("read the plans: %v", err)
	}

	if len(planned.Schedules) != 2 || !planned.Schedules[0].ValidFrom.Equal(day(6)) {
		t.Fatalf("the plans read back as %+v, want the sixth first", planned.Schedules)
	}

	if want := model.DefaultWeek(7.5); planned.Before != want {
		t.Errorf("the week before the first plan is %v, want %v", planned.Before, want)
	}
}

// A plan starting on the same day as another replaces it rather than standing
// beside it, so there is never a question which of two applies.
func TestAPlanFromTheSameDayReplacesTheOld(t *testing.T) {
	f := newPlannedFixture(t)

	f.plan(t, day(13), model.WeekHours{8, 8, 8, 8, 8, 0, 0})
	f.plan(t, day(13), model.WeekHours{6, 6, 6, 6, 6, 0, 0})

	planned, err := f.weeks.Planned(context.Background(), f.userID)
	if err != nil {
		t.Fatalf("read the plans: %v", err)
	}

	if len(planned.Schedules) != 1 || planned.Schedules[0].Hours[0] != 6 {
		t.Errorf("two plans from one day read back as %+v, want the second alone", planned.Schedules)
	}
}

// A day holds at most a day: a plan asking more of one is refused by name.
func TestAPlanAskingMoreThanADayIsRefused(t *testing.T) {
	f := newPlannedFixture(t)

	_, err := f.weeks.Plan(context.Background(), f.userID, day(13), model.WeekHours{25, 8, 8, 8, 8, 0, 0})

	requireKind(t, err, apperror.KindInvalid)

	detail, _ := apperror.Detail(err)
	if detail == nil || len(detail.Fields) != 1 || detail.Fields[0] != "hours" {
		t.Errorf("the refusal names %+v, want the hours", detail)
	}
}

// A day planned above the most somebody may book on it could never be met: the
// same refusal the daily target gets.
func TestAPlanAboveThePersonalMaximumIsRefused(t *testing.T) {
	f := newPlannedFixture(t)
	ctx := context.Background()

	target, most := 6.0, 8.0
	if _, err := f.users.UpdateWorkingTimes(ctx, command.UpdateWorkingTimesCommand{
		ID: f.userID, DailyTargetHours: &target, MaxDailyHours: &most,
	}); err != nil {
		t.Fatalf("set the working times: %v", err)
	}

	_, err := f.weeks.Plan(ctx, f.userID, day(13), model.WeekHours{9, 8, 8, 8, 8, 0, 0})

	requireCode(t, err, "targetOverMaximum")
}

// A plan starts on a day of the calendar, or not at all.
func TestAPlanWithoutAFirstDayIsRefused(t *testing.T) {
	f := newPlannedFixture(t)

	_, err := f.weeks.Plan(context.Background(), f.userID, time.Time{}, model.WeekHours{8, 8, 8, 8, 8, 0, 0})

	requireKind(t, err, apperror.KindInvalid)
}

// Removing a plan nobody made is "not found", and removing one hands its days
// back to the plan before it.
func TestRemovingAPlan(t *testing.T) {
	f := newPlannedFixture(t)
	ctx := context.Background()

	requireKind(t, f.weeks.Remove(ctx, f.userID, day(13)), apperror.KindNotFound)

	f.plan(t, day(13), model.WeekHours{4, 4, 4, 4, 4, 0, 0})

	if err := f.weeks.Remove(ctx, f.userID, day(13)); err != nil {
		t.Fatalf("removing the plan: %v", err)
	}

	f.book(t, day(13), 8)

	balance, err := f.overtime.Balance(ctx, f.userID, day(13), day(13))
	if err != nil {
		t.Fatalf("balance: %v", err)
	}

	if balance.TotalBalance != 0 {
		t.Errorf("after the plan was removed the Monday stands at %+.2f, want the default eight met", balance.TotalBalance)
	}
}

// One person's plans are theirs: another's balance is not measured against them,
// and they are not among the other's plans.
func TestAPlanIsOnlyItsOwnersWeek(t *testing.T) {
	f := newPlannedFixture(t)
	ctx := context.Background()

	other, err := f.users.CreateUser(ctx, command.CreateUserCommand{
		Name: "Nils", Email: "nils@example.com", Role: model.RoleUser,
	})
	if err != nil {
		t.Fatalf("seed the other account: %v", err)
	}

	f.plan(t, day(13), model.WeekHours{2, 2, 2, 2, 2, 0, 0})

	planned, err := f.weeks.Planned(ctx, other.Result.ID)
	if err != nil {
		t.Fatalf("read the other's plans: %v", err)
	}

	if len(planned.Schedules) != 0 {
		t.Errorf("the other account reads %d plan(s) that are not theirs", len(planned.Schedules))
	}
}
