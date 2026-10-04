package config_test

import (
	"math"
	"strings"
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

// The daily cap the configuration file sets is no more than a day has, as the
// same figure set on the settings screen is not.
//
// The screen refuses a cap above model.HoursPerDay, and the rule it keeps is the
// domain's: a booking is at most the daily cap, and the cap at most the hours a
// day holds. The file's reader took any positive figure, so MAX_DAILY_HOURS=30 -
// somebody thinking of a shift pattern, or of a week - let a person record
// thirty hours on one day, and the overtime balance counted all of them. A figure
// outside the range falls back to the default, as the deletion limit's does.
func TestTheDailyCapIsNoMoreThanADayHas(t *testing.T) {
	defaults := config.Load(mapConfig{})

	for _, raw := range []string{"24.5", "30", "48", "1e9"} {
		if got := config.Load(mapConfig{"MAX_DAILY_HOURS": raw}).MaxDailyHours; got != defaults.MaxDailyHours {
			t.Errorf("MAX_DAILY_HOURS=%q was read as %v, want the default %v", raw, got, defaults.MaxDailyHours)
		}
	}

	for raw, want := range map[string]float64{"24": 24, "10": 10, "7.5": 7.5} {
		if got := config.Load(mapConfig{"MAX_DAILY_HOURS": raw}).MaxDailyHours; got != want {
			t.Errorf("MAX_DAILY_HOURS=%q was read as %v, want %v", raw, got, want)
		}
	}
}

// A value that cannot be used is said, as well as replaced.
//
// The fallback is right - an installation has to start whatever its environment
// holds - and the silence was not. TLS_ENABLED=yes is not a value ParseBool
// reads, so TLS was off, and the start then told somebody who had switched it
// on that "TLS_ENABLED is false". Every setting read here fell back the same
// way, without a word.
func TestAValueThatCannotBeUsedIsNamed(t *testing.T) {
	cfg := config.Load(mapConfig{
		"TLS_ENABLED":       "yes",
		"SESSION_LIFETIME":  "12hours",
		"RATE_LIMIT":        "-3",
		"MAX_DAILY_HOURS":   "30",
		"TLS_REDIRECT_PORT": "8080",
		"SESSION_IDLE":      "0",
	})

	named := strings.Join(cfg.Unusable, "\n")

	for _, key := range []string{"TLS_ENABLED", "SESSION_LIFETIME", "RATE_LIMIT", "MAX_DAILY_HOURS"} {
		if !strings.Contains(named, key) {
			t.Errorf("%s could not be used and nothing says so: %q", key, cfg.Unusable)
		}
	}

	// A value that was used as given is not a problem, and neither is 0 where 0
	// means something: an idle timeout of 0 is no idle timeout.
	for _, key := range []string{"TLS_REDIRECT_PORT", "SESSION_IDLE"} {
		if strings.Contains(named, key) {
			t.Errorf("%s was used as given and is still reported: %q", key, cfg.Unusable)
		}
	}

	if quiet := config.Load(mapConfig{}).Unusable; len(quiet) != 0 {
		t.Errorf("an installation that sets nothing is told %q", quiet)
	}
}
