package service

import (
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

// A start does not visit the challenges already waiting unless a sweep is due.
//
// That visit on every start is what made a start cost more the more had been
// started, and the start of a sign-in needs no session. What shows it is an
// expired challenge left where it is by a start that came too soon after the
// last sweep, and taken away by the first start after the interval.
func TestAStartSweepsTheExpiredChallengesOnlyWhenASweepIsDue(t *testing.T) {
	var store challenges

	start := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)

	store.put("first", webauthn.SessionData{}, start)
	store.put("second", webauthn.SessionData{}, start.Add(30*time.Second))

	// The first has expired, and the sweep that took place with it is more than
	// an interval ago.
	store.put("third", webauthn.SessionData{}, start.Add(challengeLifetime+10*time.Second))

	if _, waiting := store.byToken["first"]; waiting {
		t.Error("a start an interval after the last sweep left an expired challenge in place")
	}

	// The second has expired now too, and the last sweep was only moments ago.
	store.put("fourth", webauthn.SessionData{}, start.Add(challengeLifetime+40*time.Second))

	if _, waiting := store.byToken["second"]; !waiting {
		t.Error("a start moments after a sweep visited every challenge waiting, which is what made " +
			"each start cost more the more had been started")
	}

	// Not reached by a sweep, and still not handed out.
	if _, ok := store.take("second", start.Add(challengeLifetime+41*time.Second)); ok {
		t.Error("an expired challenge was taken because no sweep had reached it yet")
	}

	if _, ok := store.take("fourth", start.Add(challengeLifetime+41*time.Second)); !ok {
		t.Error("a challenge within its lifetime was refused")
	}

	if _, ok := store.take("fourth", start.Add(challengeLifetime+42*time.Second)); ok {
		t.Error("a challenge was taken twice")
	}
}
