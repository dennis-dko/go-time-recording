package service_test

import (
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
)

// What a run removed is said the same way whoever started it.
//
// A caller's log is the only record of a deletion that cannot be undone, and
// the two callers each worded it for themselves: the schedule wrote
// "(3 time entries)" and the button "and its 3 time entry/entries", for the same
// event, in the one place somebody searches to find out what a run did. The
// report words it now, and both write what it gives them.
func TestARunSaysWhatItRemovedInOneWay(t *testing.T) {
	report := &service.SyncReport{
		Candidates: []service.SyncCandidate{
			{Email: "stays@example.com", Timesheets: 9},
		},
		Deleted: []service.SyncCandidate{
			{Email: "dave@example.com", Timesheets: 3},
			{Email: "erin@example.com", Timesheets: 1},
			{Email: "frank@example.com"},
		},
	}

	want := []string{
		`directory sync removed "dave@example.com" with 3 time entries`,
		`directory sync removed "erin@example.com" with 1 time entry`,
		`directory sync removed "frank@example.com" with 0 time entries`,
	}

	got := report.Removals()

	if len(got) != len(want) {
		t.Fatalf("a run that removed %d accounts says %d line(s): %q", len(want), len(got), got)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d is %q, want %q", i+1, got[i], want[i])
		}
	}

	// A run that failed before it had a report is still asked, by a caller that
	// logs first and looks at the error afterwards.
	var none *service.SyncReport

	if lines := none.Removals(); len(lines) != 0 {
		t.Errorf("a run with no report says it removed something: %q", lines)
	}

	// Proposed is not removed: a preview, or a run a guard refused, deleted nobody.
	if lines := (&service.SyncReport{Candidates: report.Candidates}).Removals(); len(lines) != 0 {
		t.Errorf("a run that only proposed an account says it removed it: %q", lines)
	}
}
