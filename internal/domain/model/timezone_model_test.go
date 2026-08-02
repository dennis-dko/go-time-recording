package model_test

import (
	"testing"
	"time"

	_ "time/tzdata"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// The zone decides which calendar day a booking lands on, so a wrong answer
// here does not merely look odd - it moves recorded hours between days, and
// the totals people are paid against with them.

func TestUserTimezoneWinsOverTheInstance(t *testing.T) {
	user := &model.User{Timezone: "Pacific/Auckland"}

	if got := user.TimezoneOf("Europe/Berlin").String(); got != "Pacific/Auckland" {
		t.Errorf("expected the user's own zone, got %q", got)
	}
}

func TestEmptyUserTimezoneFollowsTheInstance(t *testing.T) {
	user := &model.User{}

	if got := user.TimezoneOf("Europe/Berlin").String(); got != "Europe/Berlin" {
		t.Errorf("expected the instance zone, got %q", got)
	}
}

func TestBothEmptyFallsBackToUTC(t *testing.T) {
	user := &model.User{}

	if got := user.TimezoneOf("").String(); got != "UTC" {
		t.Errorf("expected UTC, got %q", got)
	}
}

// A name that stopped resolving - a restored database, a zone removed upstream
// - must degrade rather than take the application down.
func TestUnknownZonesFallThroughInOrder(t *testing.T) {
	cases := []struct {
		name           string
		user, instance string
		want           string
	}{
		{"unknown user zone falls to the instance", "Europe/Munich", "Europe/Berlin", "Europe/Berlin"},
		{"both unknown falls to UTC", "Europe/Munich", "Mars/Olympus", "UTC"},
		{"unknown instance zone with no user zone", "", "Nowhere/Nothing", "UTC"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := model.EffectiveTimezoneName(tc.user, tc.instance); got != tc.want {
				t.Errorf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestIsSupportedTimezone(t *testing.T) {
	valid := []string{"UTC", "Europe/Berlin", "America/New_York", "Pacific/Auckland"}
	for _, name := range valid {
		if !model.IsSupportedTimezone(name) {
			t.Errorf("%q should be accepted", name)
		}
	}

	// "Europe/Munich" is the trap: it reads like a real zone but is not one, and
	// storing it would leave every date silently computed in UTC.
	invalid := []string{"", "Europe/Munich", "GMT+2", "Berlin", "../etc/passwd"}
	for _, name := range invalid {
		if model.IsSupportedTimezone(name) {
			t.Errorf("%q should be rejected", name)
		}
	}
}

// The point of the whole feature: at the same instant, two people in different
// zones are on different calendar days.
func TestTheSameInstantIsADifferentDayInDifferentZones(t *testing.T) {
	// 22:30 UTC: already tomorrow in Berlin, still today in New York.
	instant := time.Date(2026, 8, 1, 22, 30, 0, 0, time.UTC)

	berlin := instant.In(model.EffectiveTimezone("Europe/Berlin", ""))
	newYork := instant.In(model.EffectiveTimezone("America/New_York", ""))

	if got := berlin.Format("2006-01-02"); got != "2026-08-02" {
		t.Errorf("expected 2026-08-02 in Berlin, got %s", got)
	}

	if got := newYork.Format("2006-01-02"); got != "2026-08-01" {
		t.Errorf("expected 2026-08-01 in New York, got %s", got)
	}
}

// The tz database has to be compiled in, or every zone silently resolves to UTC
// on a host with no zoneinfo files - a scratch container, for instance.
func TestTimezoneDatabaseIsAvailable(t *testing.T) {
	location, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatalf("the timezone database must be available: %v", err)
	}

	// Summer in Berlin is UTC+2; getting UTC back would mean the lookup
	// succeeded in name only.
	_, offset := time.Date(2026, 8, 1, 12, 0, 0, 0, location).Zone()
	if offset != 2*60*60 {
		t.Errorf("expected UTC+2 in Berlin in August, got %d seconds", offset)
	}
}
