package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
)

// on is a day of 2026 by month and day, for the holidays that are not in July.
func on(month time.Month, d int) time.Time {
	return time.Date(2026, month, d, 0, 0, 0, 0, time.UTC)
}

// The week of Whit Monday is four days of leave in a state that keeps it, and
// five where the installation keeps no holidays at all.
func TestLeaveLeavesOutThePublicHolidays(t *testing.T) {
	for region, want := range map[string]int{"BY": 4, "": 5} {
		f := newPlannedFixture(t)
		f.region = region

		recorded, err := f.away.Record(context.Background(), f.userID,
			on(time.May, 25), on(time.May, 29), model.AbsenceVacation)
		if err != nil {
			t.Fatalf("record with region %q: %v", region, err)
		}

		if len(recorded) != want {
			t.Errorf("Whit week with region %q was recorded as %v, want %d days",
				region, daysOf(recorded), want)
		}
	}
}

// The days of a week name the holidays among them.
func TestTheDaysNameTheirHolidays(t *testing.T) {
	f := newPlannedFixture(t)
	f.region = "BY"

	days, err := f.overtime.Days(context.Background(), f.userID, on(time.May, 25), on(time.May, 31))
	if err != nil {
		t.Fatalf("days: %v", err)
	}

	for _, d := range days {
		want := ""
		if d.Date.Equal(on(time.May, 25)) {
			want = "whitMonday"
		}

		if d.Holiday != want {
			t.Errorf("%s reads holiday %q, want %q", d.Date.Format(time.DateOnly), d.Holiday, want)
		}
	}
}

// A person's own choice wins over the installation's: their own state's holidays,
// or none at all.
func TestAPersonsOwnRegionWinsOverTheInstallations(t *testing.T) {
	f := newPlannedFixture(t)
	f.region = "BY"
	ctx := context.Background()

	holidayOn := func(day time.Time) string {
		t.Helper()

		days, err := f.overtime.Days(ctx, f.userID, day, day)
		if err != nil {
			t.Fatalf("days: %v", err)
		}

		return days[0].Holiday
	}

	// Corpus Christi is kept in Bavaria and not in Berlin.
	if got := holidayOn(on(time.June, 4)); got != "corpusChristi" {
		t.Errorf("following the installation, Corpus Christi reads %q", got)
	}

	if err := f.userRepo.SetPreference(ctx, f.userID, repository.PreferenceHolidayRegion, "BE"); err != nil {
		t.Fatalf("choose Berlin: %v", err)
	}

	if got := holidayOn(on(time.June, 4)); got != "" {
		t.Errorf("somebody in Berlin has Corpus Christi as %q, want no holiday", got)
	}

	if err := f.userRepo.SetPreference(ctx, f.userID, repository.PreferenceHolidayRegion, model.NoHolidays); err != nil {
		t.Fatalf("choose none: %v", err)
	}

	if got := holidayOn(on(time.May, 25)); got != "" {
		t.Errorf("somebody keeping no holidays has Whit Monday as %q", got)
	}
}
