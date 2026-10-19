package model_test

import (
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// What a day planned at eight hours asks with each kind of absence on it: leave,
// illness and a local holiday nothing, half a day of leave half, and a day taken
// against overtime the whole day - which is what takes it off the balance.
func TestWhatAnAbsenceAsksOfItsDay(t *testing.T) {
	for kind, want := range map[string]float64{
		model.AbsenceVacation:     0,
		model.AbsenceHalfVacation: 4,
		model.AbsenceSick:         0,
		model.AbsenceFlexDay:      8,
		model.AbsenceLocalHoliday: 0,
	} {
		if got := (model.Absence{Kind: kind}).Target(8); got != want {
			t.Errorf("a day of %s asks %.2f of eight hours, want %.2f", kind, got, want)
		}
	}

	if len(model.AbsenceKinds()) != 5 {
		t.Errorf("there are %d kinds of absence, and this case names five", len(model.AbsenceKinds()))
	}
}
