package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// Every day of a week is there, the empty ones included, with what was booked on
// it and what it asked - which is what a picture of a week is drawn from.
func TestEveryDayOfAWeekIsThereWithItsTarget(t *testing.T) {
	f := newPlannedFixture(t)

	f.book(t, day(13), 8) // Monday
	f.book(t, day(15), 3) // Wednesday
	f.book(t, day(15), 1)

	days, err := f.overtime.Days(context.Background(), f.userID, day(13), day(19))
	if err != nil {
		t.Fatalf("days: %v", err)
	}

	if len(days) != 7 {
		t.Fatalf("a week came back as %d day(s), want 7", len(days))
	}

	want := []struct {
		booked, target float64
	}{
		{8, 8}, {0, 8}, {4, 8}, {0, 8}, {0, 8}, {0, 0}, {0, 0},
	}

	for i, d := range days {
		if !d.Date.Equal(day(13 + i)) {
			t.Errorf("day %d is %s, want %s", i, d.Date.Format(time.DateOnly), day(13+i).Format(time.DateOnly))
		}

		if d.Booked != want[i].booked || d.Target != want[i].target {
			t.Errorf("%s: booked %.2f of %.2f, want %.2f of %.2f",
				d.Date.Format(time.DateOnly), d.Booked, d.Target, want[i].booked, want[i].target)
		}
	}
}

// The days follow the plan in force on each of them, as the balance does.
func TestTheDaysFollowThePlan(t *testing.T) {
	f := newPlannedFixture(t)

	f.plan(t, day(13), model.WeekHours{8, 8, 8, 8, 6, 0, 0})

	days, err := f.overtime.Days(context.Background(), f.userID, day(17), day(17))
	if err != nil {
		t.Fatalf("days: %v", err)
	}

	if len(days) != 1 || days[0].Target != 6 {
		t.Errorf("the planned Friday reads %+v, want a target of six", days)
	}
}

// A range is bounded by what one picture can show: a year and a week, which is
// what a year drawn as whole weeks spans. Anything longer is refused by name.
func TestTheDaysAreBoundedToAYearOfWeeks(t *testing.T) {
	f := newPlannedFixture(t)
	ctx := context.Background()

	if _, err := f.overtime.Days(ctx, f.userID, day(1), day(1).AddDate(0, 0, 370)); err != nil {
		t.Errorf("fifty-three weeks were refused: %v", err)
	}

	_, err := f.overtime.Days(ctx, f.userID, day(1), day(1).AddDate(0, 0, 371))
	requireKind(t, err, apperror.KindInvalid)
	requireCode(t, err, "rangeTooLong")
}

// A range that ends before it begins is the same refusal the balance gives.
func TestTheDaysRefuseAnInvertedRange(t *testing.T) {
	f := newPlannedFixture(t)

	_, err := f.overtime.Days(context.Background(), f.userID, day(20), day(13))

	requireCode(t, err, "rangeInverted")
}
