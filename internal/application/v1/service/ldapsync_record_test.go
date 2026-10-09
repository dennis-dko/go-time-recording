package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// A run that removed somebody leaves a record of it that no log level can switch
// off - how many, how much went with them and when, and never whom.
//
// The log was the only record of a deletion, and a level above WARN dropped it.
// The record holds no name or address because the people a run removed are the
// people the purge exists to erase: the counts are what has to survive, and the
// type the run is recorded as has nowhere to put anything else.
func TestARunThatChangedSomethingIsRecordedWithoutWhom(t *testing.T) {
	ctx := context.Background()
	f := newSyncFixture(t, 1.0)

	leaver := externalUser(t, f.fixture, "leaver@example.com")

	for day := 1; day <= 3; day++ {
		if _, err := f.timesheetRepo.Save(ctx, &model.Timesheet{
			UserID: leaver, Date: model.CalendarDay(time.Date(2026, 9, day, 0, 0, 0, 0, time.UTC)),
			DurationHours: 2,
		}); err != nil {
			t.Fatalf("booking an entry: %v", err)
		}
	}

	f.directory.users = []service.ExternalUser{{ID: "arrives", Email: "arrives@example.com"}}

	if _, err := f.sync.Sync(ctx); err != nil {
		t.Fatalf("sync: %v", err)
	}

	runs, err := f.runs.Latest(ctx, 10)
	if err != nil {
		t.Fatalf("reading the runs: %v", err)
	}

	if len(runs) != 1 {
		t.Fatalf("the run left %d records, want 1", len(runs))
	}

	if run := runs[0]; run.Confirmed || run.Deleted != 1 || run.EntriesDeleted != 3 ||
		run.Created != 1 || run.RanAt.IsZero() {
		t.Errorf("the run was recorded as %+v; want an unconfirmed run, dated, that removed one "+
			"account with its 3 entries and added one", *run)
	}
}

// A run somebody confirmed against a preview is recorded as confirmed.
func TestAConfirmedRunIsRecordedAsConfirmed(t *testing.T) {
	ctx := context.Background()
	f := newSyncFixture(t, 1.0)

	leaver := externalUser(t, f.fixture, "leaver@example.com")
	f.directory.users = []service.ExternalUser{{ID: "kept", Email: "kept@example.com"}}

	if _, err := f.sync.SyncAsConfirmed(ctx, []uint{leaver}); err != nil {
		t.Fatalf("confirmed sync: %v", err)
	}

	runs, err := f.runs.Latest(ctx, 10)
	if err != nil {
		t.Fatalf("reading the runs: %v", err)
	}

	if len(runs) != 1 || !runs[0].Confirmed || runs[0].Deleted != 1 {
		t.Errorf("the confirmed run left %d record(s): %+v", len(runs), runs)
	}
}

// A preview, a run a guard refused and a run that found nothing to do leave no
// record: what is recorded is what was done to accounts, and here nothing was.
func TestARunThatChangedNothingLeavesNoRecord(t *testing.T) {
	ctx := context.Background()
	f := newSyncFixture(t, 1.0)

	externalUser(t, f.fixture, "stays@example.com")
	f.directory.users = []service.ExternalUser{{ID: "stays", Email: "stays@example.com"}}

	if _, err := f.sync.Preview(ctx); err != nil {
		t.Fatalf("preview: %v", err)
	}

	if _, err := f.sync.Sync(ctx); err != nil {
		t.Fatalf("a run with nothing to do: %v", err)
	}

	f.directory.users = nil

	if report, err := f.sync.Sync(ctx); err != nil || report.Aborted == "" {
		t.Fatalf("a directory answering with nobody was not refused: %v, %+v", err, report)
	}

	runs, err := f.runs.Latest(ctx, 10)
	if err != nil {
		t.Fatalf("reading the runs: %v", err)
	}

	if len(runs) != 0 {
		t.Errorf("runs that changed nothing left %d record(s): %+v", len(runs), runs)
	}
}
