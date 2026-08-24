package model_test

import (
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// These settings are administered from the very screen they govern, so the
// bounds below are not tidiness: they are what stops an administrator locking
// themselves - and everyone else - out of the instance.

func envDefaults() model.Limits {
	return model.Limits{
		SessionLifetimeHours:   12,
		MaxDailyHours:          24,
		RateLimit:              30,
		RateLimitWindowSeconds: 60,
		LDAPSyncMaxDeleteRatio: 0.5,
	}
}

// Nothing overridden means the environment still decides everything.
func TestEmptyOperationalKeepsTheEnvironment(t *testing.T) {
	got := model.Operational{}.Resolve(envDefaults())

	if got != envDefaults() {
		t.Errorf("expected the environment's values untouched, got %+v", got)
	}
}

func TestOverridesReplaceOnlyWhatIsSet(t *testing.T) {
	got := model.Operational{
		MaxDailyHours: new(10.0),
		RateLimit:     new(100),
	}.Resolve(envDefaults())

	if got.MaxDailyHours != 10 {
		t.Errorf("expected the override, got %v", got.MaxDailyHours)
	}

	if got.RateLimit != 100 {
		t.Errorf("expected the override, got %v", got.RateLimit)
	}

	// Untouched fields must not drift to a zero value.
	if got.SessionLifetimeHours != 12 {
		t.Errorf("an unset field must keep the environment's value, got %v", got.SessionLifetimeHours)
	}

}

// Zero is a real choice for two of these, which is the reason every field is a
// pointer: a plain zero could not be told apart from "not configured here".
func TestZeroIsAnOverrideNotAnAbsence(t *testing.T) {
	got := model.Operational{
		LDAPSyncMaxDeleteRatio: new(0.0),
	}.Resolve(envDefaults())

	if got.LDAPSyncMaxDeleteRatio != 0 {
		t.Errorf("a deliberate 0 must switch the check off, got %v", got.LDAPSyncMaxDeleteRatio)
	}
}

func TestValidationRejectsValuesThatWouldBreakTheInstance(t *testing.T) {
	cases := []struct {
		name  string
		given model.Operational
		field string
	}{
		{
			"a session measured in seconds would sign people out mid-click",
			model.Operational{SessionLifetimeHours: new(0.001)}, "sessionLifetimeHours",
		},
		{
			"a session lasting months is a permanent credential, not a session",
			model.Operational{SessionLifetimeHours: new(10000.0)}, "sessionLifetimeHours",
		},
		{
			"a rate limit this low would refuse the administrator's own sign-in",
			model.Operational{RateLimit: new(1)}, "rateLimit",
		},
		{
			"more hours than a day has cannot be worked in one",
			model.Operational{MaxDailyHours: new(48.0)}, "maxDailyHours",
		},
		{
			"a daily cap of zero would refuse every booking",
			model.Operational{MaxDailyHours: new(0.0)}, "maxDailyHours",
		},
		{
			"a ratio above one cannot mean anything",
			model.Operational{LDAPSyncMaxDeleteRatio: new(1.5)}, "ldapSyncMaxDeleteRatio",
		},
		{
			"a negative ratio cannot mean anything either",
			model.Operational{LDAPSyncMaxDeleteRatio: new(-0.1)}, "ldapSyncMaxDeleteRatio",
		},
		{
			"a window of zero seconds is not a window",
			model.Operational{RateLimitWindowSeconds: new(0)}, "rateLimitWindowSeconds",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			invalid := tc.given.InvalidOperationalFields()

			if len(invalid) == 0 {
				t.Fatalf("expected %s to be rejected", tc.field)
			}

			if invalid[0] != tc.field {
				t.Errorf("expected %q to be named, got %v", tc.field, invalid)
			}
		})
	}
}

func TestValidationAcceptsReasonableValues(t *testing.T) {
	given := model.Operational{
		SessionLifetimeHours:   new(8.0),
		MaxDailyHours:          new(10.0),
		RateLimit:              new(60),
		RateLimitWindowSeconds: new(60),
		LDAPSyncMaxDeleteRatio: new(0.25),
	}

	if invalid := given.InvalidOperationalFields(); len(invalid) > 0 {
		t.Errorf("expected these to be accepted, but %v were rejected", invalid)
	}
}
