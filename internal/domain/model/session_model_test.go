package model_test

import (
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// A session ends for two different reasons, and they measure different things.
//
// The lifetime is absolute and starts at the sign-in: a bound on how long one
// act of proving who you are is worth. Idleness is measured from the last use
// and asks whether anybody is still there. A person working all morning keeps
// their session by the second rule and loses it by the first; the same person
// going home at noon loses it by the second while the first would have let them
// back in.
func TestASessionEndsWhenItsTimeIsUpOrNobodyIsThere(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)

	worked := &model.Session{
		CreatedAt:  now.Add(-5 * time.Hour),
		ExpiresAt:  now.Add(7 * time.Hour),
		LastSeenAt: now.Add(-time.Minute),
	}

	if worked.Expired(now) {
		t.Error("a session five hours into a twelve hour lifetime is reported as expired")
	}

	if worked.Idle(now, 30*time.Minute) {
		t.Error("a session used a minute ago is reported as idle after thirty")
	}

	// Same session, left alone over lunch.
	left := &model.Session{
		CreatedAt:  now.Add(-5 * time.Hour),
		ExpiresAt:  now.Add(7 * time.Hour),
		LastSeenAt: now.Add(-90 * time.Minute),
	}

	if left.Expired(now) {
		t.Error("the lifetime is reported as run out when it has five hours left")
	}

	if !left.Idle(now, 30*time.Minute) {
		t.Error("a session untouched for ninety minutes is not idle after thirty")
	}

	// And the other way round: in constant use, but the sign-in itself is old.
	stale := &model.Session{
		CreatedAt:  now.Add(-13 * time.Hour),
		ExpiresAt:  now.Add(-time.Hour),
		LastSeenAt: now,
	}

	if !stale.Expired(now) {
		t.Error("a session past its expiry is not reported as expired")
	}

	if stale.Idle(now, 30*time.Minute) {
		t.Error("a session used just now is reported as idle")
	}
}

// No timeout is what an installation has until somebody sets one.
//
// Signing people out of a screen they left open is a decision about how an
// office works, not a default worth imposing on every installation on the day it
// updates - so zero has to mean "never", not "immediately", which is what a
// plain comparison against a zero duration would have made of it.
func TestAnIdleTimeoutOfZeroNeverEndsAnything(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)

	// Untouched for a week, which is as idle as a session gets.
	forgotten := &model.Session{
		CreatedAt:  now.Add(-7 * 24 * time.Hour),
		ExpiresAt:  now.Add(time.Hour),
		LastSeenAt: now.Add(-7 * 24 * time.Hour),
	}

	if forgotten.Idle(now, 0) {
		t.Error("a session is ended for idleness on an installation that has set no " +
			"idle timeout")
	}

	// And a negative one, which nothing should be able to store but which a
	// hand-edited row could hold.
	if forgotten.Idle(now, -time.Hour) {
		t.Error("a negative timeout ends sessions instead of meaning nothing")
	}
}
