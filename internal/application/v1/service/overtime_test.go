package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/memory"
)

// overtimeFor builds a service over the same repositories the fixture uses.
func overtimeFor(f *fixture) *service.OvertimeService {
	return service.NewOvertimeService(f.timesheetRepo, f.userRepo)
}

func TestOvertimeUsesTheUsersOwnTarget(t *testing.T) {
	f := newFixture(t)

	// 8h booked against a 6h target leaves +2h.
	target := 6.0
	if _, err := f.users.UpdateWorkingTimes(context.Background(),
		command.UpdateWorkingTimesCommand{ID: f.userID, DailyTargetHours: &target}); err != nil {
		t.Fatalf("set working times: %v", err)
	}

	f.book(t, day(15), 8)

	balance, err := overtimeFor(f).Balance(context.Background(), f.userID, day(1), day(28))
	if err != nil {
		t.Fatalf("balance: %v", err)
	}

	if balance.DailyTarget != target {
		t.Errorf("expected target %.1f, got %.1f", target, balance.DailyTarget)
	}

	if balance.TotalBalance != 2 {
		t.Errorf("expected +2h overtime, got %.2f", balance.TotalBalance)
	}
}

func TestOvertimeFallsBackToTheDefaultTarget(t *testing.T) {
	f := newFixture(t)
	f.book(t, day(15), 8)

	balance, err := overtimeFor(f).Balance(context.Background(), f.userID, day(1), day(28))
	if err != nil {
		t.Fatalf("balance: %v", err)
	}

	if balance.DailyTarget != model.DefaultDailyTargetHours {
		t.Errorf("expected the default target %d, got %.1f",
			model.DefaultDailyTargetHours, balance.DailyTarget)
	}

	// 8h booked against the 8h default is exactly zero.
	if balance.TotalBalance != 0 {
		t.Errorf("expected a balanced day, got %.2f", balance.TotalBalance)
	}
}

// Only days with bookings count. Counting every calendar day would turn
// weekends and leave into a deficit this application cannot distinguish.
func TestOvertimeIgnoresDaysWithoutBookings(t *testing.T) {
	f := newFixture(t)

	target := 8.0
	if _, err := f.users.UpdateWorkingTimes(context.Background(),
		command.UpdateWorkingTimesCommand{ID: f.userID, DailyTargetHours: &target}); err != nil {
		t.Fatalf("set working times: %v", err)
	}

	f.book(t, day(15), 10)

	balance, err := overtimeFor(f).Balance(context.Background(), f.userID, day(1), day(28))
	if err != nil {
		t.Fatalf("balance: %v", err)
	}

	if len(balance.Days) != 1 {
		t.Fatalf("expected exactly one day with bookings, got %d", len(balance.Days))
	}

	if balance.TotalTarget != 8 {
		t.Errorf("expected the target to be counted once, got %.2f", balance.TotalTarget)
	}

	if balance.TotalBalance != 2 {
		t.Errorf("expected +2h, got %.2f", balance.TotalBalance)
	}
}

func TestOvertimeRejectsInvertedRange(t *testing.T) {
	f := newFixture(t)

	_, err := overtimeFor(f).Balance(context.Background(), f.userID, day(20), day(10))
	if err == nil {
		t.Fatal("expected an error when 'to' precedes 'from'")
	}
}

func TestWorkingTimesValidation(t *testing.T) {
	f := newFixture(t)

	tooMuch := 30.0
	_, err := f.users.UpdateWorkingTimes(context.Background(),
		command.UpdateWorkingTimesCommand{ID: f.userID, DailyTargetHours: &tooMuch})
	if err == nil {
		t.Error("a target above 24h must be rejected")
	}

	// A target above the cap could never be reached.
	target := 10.0
	maxDaily := 8.0

	_, err = f.users.UpdateWorkingTimes(context.Background(), command.UpdateWorkingTimesCommand{
		ID: f.userID, DailyTargetHours: &target, MaxDailyHours: &maxDaily,
	})
	if err == nil {
		t.Error("a target above the daily maximum must be rejected")
	}
}

// Guards that the in-memory repositories are exposed for the overtime service.
var _ = memory.NewUserRepository

// The same thing where it is felt: a balance for somebody west of UTC.
//
// The rule lives on the filter and is tested there. This is the sentence it
// stands for - eight hours short and one day missing, on the screen somebody
// checks their own overtime on, with nothing to see wrong.
func TestOvertimeCountsTheFirstDayForSomebodyWestOfUTC(t *testing.T) {
	west, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Skip("this machine has no zone database")
	}

	f := newFixture(t)

	f.book(t, day(1), 8)
	f.book(t, day(15), 8)

	// What the handler builds when nobody passed a range: the first of the
	// month, and now, both in the reader's own zone.
	from := time.Date(2026, 7, 1, 0, 0, 0, 0, west)
	to := time.Date(2026, 7, 20, 11, 0, 0, 0, west)

	balance, err := overtimeFor(f).Balance(context.Background(), f.userID, from, to)
	if err != nil {
		t.Fatalf("balance: %v", err)
	}

	if balance.TotalBooked != 16 {
		t.Errorf("counted %.1f hours over %d days, want 16 over 2 - the first of "+
			"the month is 07:00 UTC there, and the entry for it is midnight",
			balance.TotalBooked, len(balance.Days))
	}
}
