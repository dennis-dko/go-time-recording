package service_test

import (
	"context"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// correct changes an entry through the service, as the form does.
func (f *fixture) correct(t *testing.T, change command.UpdateTimesheetCommand) {
	t.Helper()

	if _, err := f.timesheets.UpdateTimesheet(context.Background(), change); err != nil {
		t.Fatalf("correct entry %d: %v", change.ID, err)
	}
}

// A correction keeps what the entry said before it, and each correction one
// version more - the most recently replaced first, saying whose entry it is.
func TestACorrectionKeepsWhatTheEntrySaidBefore(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	id := f.book(t, day(14), 2.5)

	three := 3.0
	f.correct(t, command.UpdateTimesheetCommand{ID: id, DurationHours: &three})

	written := "Login form"
	f.correct(t, command.UpdateTimesheetCommand{ID: id, Description: &written})

	history, err := f.timesheets.Revisions(ctx, id)
	if err != nil {
		t.Fatalf("read the history: %v", err)
	}

	if history.OwnerID != f.userID {
		t.Errorf("the history says it is %d's entry, want %d's", history.OwnerID, f.userID)
	}

	if len(history.Revisions) != 2 {
		t.Fatalf("two corrections left %d earlier versions, want two: %+v", len(history.Revisions), history.Revisions)
	}

	latest, first := history.Revisions[0], history.Revisions[1]

	if latest.DurationHours != 3 || latest.Description != nil {
		t.Errorf("the version the second correction replaced reads %+v, want three hours and no description", latest)
	}

	if first.DurationHours != 2.5 || !first.Date.Equal(day(14)) || first.ProjectID == nil || *first.ProjectID != f.projectID {
		t.Errorf("the version as first booked reads %+v, want 2.5 hours on the 14th on the project", first)
	}

	if latest.ReplacedAt.IsZero() || latest.ReplacedAt.Before(first.ReplacedAt) {
		t.Errorf("the versions were replaced at %v and %v, want the latest first", latest.ReplacedAt, first.ReplacedAt)
	}
}

// A save that changes nothing corrects nothing, and keeps no version.
func TestASaveThatChangesNothingKeepsNoRevision(t *testing.T) {
	f := newFixture(t)

	id := f.book(t, day(14), 2.5)

	same := 2.5
	f.correct(t, command.UpdateTimesheetCommand{ID: id, DurationHours: &same})

	history, err := f.timesheets.Revisions(context.Background(), id)
	if err != nil {
		t.Fatalf("read the history: %v", err)
	}

	if len(history.Revisions) != 0 {
		t.Errorf("an unchanged save left %+v", history.Revisions)
	}
}

// An entry taken away takes its history with it: nothing is left of it to read.
func TestARemovedEntryTakesItsHistoryWithIt(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	id := f.book(t, day(14), 2.5)

	three := 3.0
	f.correct(t, command.UpdateTimesheetCommand{ID: id, DurationHours: &three})

	if err := f.timesheets.DeleteTimesheet(ctx, command.DeleteTimesheetCommand{ID: id}); err != nil {
		t.Fatalf("delete the entry: %v", err)
	}

	_, err := f.timesheets.Revisions(ctx, id)
	requireKind(t, err, apperror.KindNotFound)

	left, err := f.timesheetRepo.Revisions(ctx, id)
	if err != nil || len(left) != 0 {
		t.Errorf("the history of a deleted entry is still kept: %+v, %v", left, err)
	}
}
