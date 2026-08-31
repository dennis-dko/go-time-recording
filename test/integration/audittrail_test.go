//go:build integration

package integration

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// An entry says when it was recorded and when it was last corrected.
//
// The hours in this table are what somebody is paid for, and until now a row
// carried no trace of when it appeared or whether it had been changed since. A
// figure that is disputed a month later could not be told apart from one that had
// been quietly corrected the day after it was booked - the only date on the row is
// the day the work was done, which is the one thing that stays the same when
// somebody edits the number.
//
// There is deliberately no "who": there is no timesheets:write:all in this
// application, so nobody can book or change time in another person's name, and a
// column recording the actor would hold the owner's own id in every row.
//
// An API test rather than a repository one, because what has to hold is that the
// two moments survive the whole round trip - written by the repository, carried
// through the result and the DTO, and rendered as a time of day rather than a bare
// date, which is what the wire type this application already had would have done.

// recordedAndCorrected reads the two audit fields, failing if either is missing.
func recordedAndCorrected(t *testing.T, entry timesheetResponse) (time.Time, time.Time) {
	t.Helper()

	if entry.CreatedAt == nil {
		t.Fatal("the entry carries no createdAt, so nothing records when it was booked")
	}

	if entry.UpdatedAt == nil {
		t.Fatal("the entry carries no updatedAt, so a correction leaves no trace")
	}

	created, err := time.Parse(time.RFC3339, *entry.CreatedAt)
	if err != nil {
		t.Fatalf("createdAt %q is not a timestamp: %v", *entry.CreatedAt, err)
	}

	updated, err := time.Parse(time.RFC3339, *entry.UpdatedAt)
	if err != nil {
		t.Fatalf("updatedAt %q is not a timestamp: %v", *entry.UpdatedAt, err)
	}

	// The wire format must carry the time of day, not only the date. Asked by
	// trying to read it as a bare date rather than by looking for midnight in the
	// parsed value: midnight is a real moment, and a booking made in the minute
	// after it would have failed this check once a day for no reason.
	if _, err := time.Parse(time.DateOnly, *entry.CreatedAt); err == nil {
		t.Errorf("createdAt %q is a bare date; a correction made the same day would "+
			"be indistinguishable from the booking it corrected", *entry.CreatedAt)
	}

	return created, updated
}

func TestABookedEntrySaysWhenItWasRecorded(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")
	anna := a.signInAsUser(admin, "Anna", "anna@example.com")

	before := time.Now().Add(-time.Minute)

	entry := entryOf(t, anna, map[string]any{
		"date":          "2026-08-31",
		"durationHours": 2.5,
		"description":   "the first version of the figure",
	})

	created, updated := recordedAndCorrected(t, entry)

	if created.Before(before) {
		t.Errorf("createdAt is %s, which is before this test started", created)
	}

	// Nothing has corrected it yet, so the two moments are the same one.
	if !created.Equal(updated) {
		t.Errorf("an entry nobody has corrected reports createdAt %s and updatedAt %s; "+
			"they should be the same moment", created, updated)
	}
}

func TestCorrectingAnEntryMovesOnlyTheCorrectionTime(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")
	anna := a.signInAsUser(admin, "Anna", "anna@example.com")

	entry := entryOf(t, anna, map[string]any{
		"date":          "2026-08-31",
		"durationHours": 2.5,
		"description":   "the first version of the figure",
	})

	created, _ := recordedAndCorrected(t, entry)

	// MySQL's DATETIME keeps whole seconds, so a correction inside the same second
	// as the booking is genuinely indistinguishable from it in the database. One
	// second is resolution enough for what this records - the point is that the
	// column moves at all - and waiting is the only honest way to prove it does.
	time.Sleep(1100 * time.Millisecond)

	var corrected timesheetResponse

	anna.must(anna.api(http.MethodPut, fmt.Sprintf("/timesheets/%d", entry.ID), map[string]any{
		"durationHours": 3.5,
	}), http.StatusOK).Data(t, &corrected)

	if corrected.DurationHours != 3.5 {
		t.Fatalf("the correction did not take: hours are %v", corrected.DurationHours)
	}

	createdAgain, updatedAgain := recordedAndCorrected(t, corrected)

	if !createdAgain.Equal(created) {
		t.Errorf("correcting the entry moved createdAt from %s to %s; when it was "+
			"booked does not change when the figure does", created, createdAgain)
	}

	if !updatedAgain.After(created) {
		t.Errorf("updatedAt is %s and the entry was booked at %s; a correction that "+
			"does not move it leaves no trace at all", updatedAgain, created)
	}
}
