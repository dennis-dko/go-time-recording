package config_test

import (
	"math"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/infrastructure/config"
)

// A figure in the configuration that is not a finite number falls back to the
// default, as one that cannot be read at all does.
//
// ParseFloat reads "NaN" and "Inf", and NaN compares false against every bound -
// so a reader that checks "below zero or above one" lets it through. TRACER_RATIO
// already catches it, for the reason written there: the value is sent to the
// settings screen, JSON cannot encode it, and the screen then renders nothing.
// The two readers beside it did not, and what they feed is worse than a blank
// screen. The deletion limit is the guard on how much of the directory one
// synchronisation may remove, and NaN compares false against it too; the daily
// cap is what a booking is measured against, and NaN or infinity switches it off.
func TestAFigureThatIsNotAFiniteNumberFallsBack(t *testing.T) {
	defaults := config.Load(mapConfig{})

	for _, raw := range []string{"NaN", "nan", "Inf", "+Inf", "-Inf", "infinity"} {
		got := config.Load(mapConfig{
			"LDAP_SYNC_MAX_DELETE_RATIO": raw,
			"MAX_DAILY_HOURS":            raw,
		})

		for name, pair := range map[string][2]float64{
			"LDAP_SYNC_MAX_DELETE_RATIO": {got.LDAPSyncMaxDeleteRatio, defaults.LDAPSyncMaxDeleteRatio},
			"MAX_DAILY_HOURS":            {got.MaxDailyHours, defaults.MaxDailyHours},
		} {
			value, fallback := pair[0], pair[1]

			if math.IsNaN(value) || math.IsInf(value, 0) || value != fallback {
				t.Errorf("%s=%q was read as %v, want the default %v", name, raw, value, fallback)
			}
		}
	}
}
