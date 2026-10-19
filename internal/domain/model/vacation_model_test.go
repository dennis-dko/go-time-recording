package model_test

import (
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// A year gives the latest entitlement from it or before, and nothing before the
// first one.
func TestAYearGivesTheEntitlementInForceInIt(t *testing.T) {
	entitlements := []model.VacationEntitlement{{FromYear: 2027, Days: 30}, {FromYear: 2025, Days: 28}}

	for year, want := range map[int]float64{2025: 28, 2026: 28, 2027: 30, 2040: 30} {
		if got, found := model.EntitlementIn(year, entitlements); !found || got != want {
			t.Errorf("%d gives %.1f days (found: %v), want %.1f", year, got, found, want)
		}
	}

	if _, found := model.EntitlementIn(2024, entitlements); found {
		t.Error("the year before the first entitlement gives one")
	}
}

// Leave uses a day, half a day of it a half, and nothing else uses the allowance;
// what remains is what the year gave and carried, less what is taken and planned.
func TestWhatLeaveUsesAndWhatRemains(t *testing.T) {
	for kind, want := range map[string]float64{
		model.AbsenceVacation: 1, model.AbsenceHalfVacation: 0.5, model.AbsenceSick: 0,
		model.AbsenceFlexDay: 0, model.AbsenceLocalHoliday: 0,
	} {
		if got := (model.Absence{Kind: kind}).VacationDays(); got != want {
			t.Errorf("a day of %s uses %.1f of the allowance, want %.1f", kind, got, want)
		}
	}

	year := model.VacationYear{Entitlement: 28, CarriedOver: 2, Taken: 18.5, Planned: 4}

	if got := year.Remaining(); got != 7.5 {
		t.Errorf("28 and 2 carried, less 18.5 taken and 4 planned, leaves %.1f, want 7.5", got)
	}
}
