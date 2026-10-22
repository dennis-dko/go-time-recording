package model_test

import (
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

func hours(h float64) *float64 { return &h }

// A balance is read against the bounds its owner set, and only past one of them:
// a balance standing exactly on a bound is inside it, and an account that set no
// bound is never outside anything.
func TestABalanceIsReadAgainstItsOwnersBounds(t *testing.T) {
	bounded := &model.User{BalanceUpperLimit: hours(40), BalanceLowerLimit: hours(-20)}

	cases := []struct {
		name    string
		user    *model.User
		balance float64
		want    string
	}{
		{"above the upper bound", bounded, 43.5, model.BalanceAbove},
		{"on the upper bound", bounded, 40, ""},
		{"between the bounds", bounded, 12, ""},
		{"on the lower bound", bounded, -20, ""},
		{"below the lower bound", bounded, -20.25, model.BalanceBelow},
		{"no bounds at all", &model.User{}, 400, ""},
		{"only a lower bound", &model.User{BalanceLowerLimit: hours(-5)}, 400, ""},
		{"an upper bound of nothing at all", &model.User{BalanceUpperLimit: hours(0)}, 0.5, model.BalanceAbove},
	}

	for _, tc := range cases {
		if got := tc.user.BalanceOutside(tc.balance); got != tc.want {
			t.Errorf("%s: a balance of %.2f reads %q, want %q", tc.name, tc.balance, got, tc.want)
		}
	}
}

// A stopwatch is mentioned after its owner's chosen hours, after the default for
// somebody who never chose, and never for somebody who chose zero.
func TestAStopwatchIsMentionedAfterItsOwnersHours(t *testing.T) {
	never, six := 0, 6

	for _, tc := range []struct {
		name string
		user *model.User
		want int
	}{
		{"nobody chose", &model.User{}, model.DefaultTimerAlertHours},
		{"six hours chosen", &model.User{TimerAlertHours: &six}, 6},
		{"never chosen", &model.User{TimerAlertHours: &never}, 0},
	} {
		if got := tc.user.EffectiveTimerAlertHours(); got != tc.want {
			t.Errorf("%s: mentioned after %d hours, want %d", tc.name, got, tc.want)
		}
	}
}
