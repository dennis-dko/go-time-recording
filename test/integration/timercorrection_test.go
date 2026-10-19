//go:build integration

package integration

import (
	"math"
	"net/http"
	"testing"
	"time"
)

// Correcting a clock: moving its start, and booking it up to the time the work
// ended. The unit tests hold the rules; these hold the wire - how a moment is written, which zone a time without
// an offset is read in, and what a refusal leaves behind.

// startedAt reads the running clock's start.
func startedAt(t *testing.T, c *client) time.Time {
	t.Helper()

	timer := runningTimer(t, c)
	if !timer.Running || timer.StartedAt == nil {
		t.Fatalf("no clock is running: %+v", timer)
	}

	at, err := time.Parse(time.RFC3339, *timer.StartedAt)
	if err != nil {
		t.Fatalf("the start %q is not RFC 3339: %v", *timer.StartedAt, err)
	}

	return at
}

// A start given with its offset is taken as it stands, and the booking measures
// from it - which is what makes a late press of the button correctable without
// waiting two hours for a test.
func TestTheStartOfTheClockCanBeCorrected(t *testing.T) {
	t.Parallel()

	_, _, worker := startWithWorker(t)

	worker.must(worker.api(http.MethodPost, "/me/timer", nil), http.StatusCreated, http.StatusOK)

	earlier := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)

	worker.must(worker.api(http.MethodPut, "/me/timer",
		map[string]string{"startedAt": earlier.Format(time.RFC3339)}), http.StatusOK)

	if got := startedAt(t, worker); !got.Equal(earlier) {
		t.Fatalf("the clock reads as started at %v, want %v", got, earlier)
	}

	var entry timesheetResponse
	worker.must(worker.api(http.MethodPost, "/me/timer/stop", nil),
		http.StatusCreated, http.StatusOK).Data(t, &entry)

	if entry.DurationHours < 1.99 || entry.DurationHours > 2.01 {
		t.Errorf("the booking holds %.4f h, want the two hours since the corrected start", entry.DurationHours)
	}
}

// A start without an offset is a time on a wall clock, and the wall clock is the
// caller's own. Tokyo is nine hours from UTC all year, so the two readings could
// not be confused by accident.
func TestAStartWithoutAnOffsetIsReadInTheCallersZone(t *testing.T) {
	t.Parallel()

	_, _, worker := startWithWorker(t)

	const zone = "Asia/Tokyo"

	location, err := time.LoadLocation(zone)
	if err != nil {
		t.Fatal(err)
	}

	worker.must(worker.api(http.MethodPut, "/me/timezone",
		map[string]string{"timezone": zone}), http.StatusOK)

	worker.must(worker.api(http.MethodPost, "/me/timer", nil), http.StatusCreated, http.StatusOK)

	// Three hours ago on a clock in Tokyo, to the minute, as a time field sends it.
	want := time.Now().Add(-3 * time.Hour).In(location).Truncate(time.Minute)

	worker.must(worker.api(http.MethodPut, "/me/timer",
		map[string]string{"startedAt": want.Format("2006-01-02T15:04")}), http.StatusOK)

	if got := startedAt(t, worker); !got.Equal(want) {
		t.Errorf("the clock reads as started at %v, want %v - the time read in the wrong zone",
			got.UTC(), want.UTC())
	}
}

// A start in the future is refused by name, and leaves the clock as it was.
func TestAClockCannotBeCorrectedIntoTheFuture(t *testing.T) {
	t.Parallel()

	_, _, worker := startWithWorker(t)

	worker.must(worker.api(http.MethodPost, "/me/timer", nil), http.StatusCreated, http.StatusOK)

	before := startedAt(t, worker)

	refused := worker.api(http.MethodPut, "/me/timer",
		map[string]string{"startedAt": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})

	if refused.Status != http.StatusBadRequest {
		t.Fatalf("a start in the future answered %d, want 400: %s", refused.Status, refused.Body)
	}

	if code := errorCode(t, refused); code != "timerStartInFuture" {
		t.Errorf("the refusal is coded %q, want timerStartInFuture", code)
	}

	if after := startedAt(t, worker); !after.Equal(before) {
		t.Errorf("the refused correction moved the clock from %v to %v", before, after)
	}
}

// The forgotten clock: refused without an end, as before, and booked with one - the
// hours between the start and the end, on the day it started.
func TestAForgottenClockIsBookedWithTheTimeItEnded(t *testing.T) {
	t.Parallel()

	_, _, worker := startWithWorker(t)

	worker.must(worker.api(http.MethodPost, "/me/timer", nil), http.StatusCreated, http.StatusOK)

	started := time.Now().Add(-30 * time.Hour).UTC().Truncate(time.Second)

	worker.must(worker.api(http.MethodPut, "/me/timer",
		map[string]string{"startedAt": started.Format(time.RFC3339)}), http.StatusOK)

	tooLong := worker.api(http.MethodPost, "/me/timer/stop", nil)
	if code := errorCode(t, tooLong); code != "timerTooLong" {
		t.Fatalf("stopping a thirty-hour clock now answered %d %q, want timerTooLong", tooLong.Status, code)
	}

	ended := started.Add(7*time.Hour + 45*time.Minute)

	var entry timesheetResponse
	worker.must(worker.api(http.MethodPost, "/me/timer/stop",
		map[string]string{"endedAt": ended.Format(time.RFC3339)}),
		http.StatusCreated, http.StatusOK).Data(t, &entry)

	if math.Abs(entry.DurationHours-7.75) > 1e-6 {
		t.Errorf("the booking holds %v h, want 7.75", entry.DurationHours)
	}

	// The test instance runs in UTC and the account never chose a zone, so the
	// day the clock started on is the UTC one.
	if want := started.Format(time.DateOnly); entry.Date[:10] != want {
		t.Errorf("the booking is on %s, want the day the clock started, %s", entry.Date, want)
	}

	if timer := runningTimer(t, worker); timer.Running {
		t.Error("the clock survived being booked")
	}
}

// An end time that cannot be read is a refusal, not a stop at the current moment:
// that would book a duration nobody asked for, with nothing to show it happened.
func TestAnUnreadableEndTimeIsRefusedRatherThanStoppingNow(t *testing.T) {
	t.Parallel()

	_, _, worker := startWithWorker(t)

	worker.must(worker.api(http.MethodPost, "/me/timer", nil), http.StatusCreated, http.StatusOK)

	before := timesheetCount(t, worker)

	refused := worker.api(http.MethodPost, "/me/timer/stop", map[string]string{"endedAt": "half past five"})

	if refused.Status != http.StatusBadRequest {
		t.Errorf("an unreadable end time answered %d, want 400: %s", refused.Status, refused.Body)
	}

	if after := timesheetCount(t, worker); after != before {
		t.Errorf("the refused stop booked %d entr(y/ies)", after-before)
	}

	if timer := runningTimer(t, worker); !timer.Running {
		t.Error("the refused stop threw the clock away")
	}
}
