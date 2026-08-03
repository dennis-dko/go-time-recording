package worker_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"
	"gofr.dev/pkg/gofr/logging"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/memory"
	"github.com/dennis-dko/go-time-recording/internal/interface/worker"
)

// The nightly sweep moves forgotten entries to submitted so they still reach
// approval. It runs unattended, at three in the morning, against everyone's
// recorded time - which is exactly the combination that makes a mistake here
// expensive and slow to notice.
//
// Two things it must not do: touch an entry somebody has already approved or
// submitted, and give up on the rest because one row failed.

// testContext builds the minimum gofr.Context the job uses: a context and a
// logger that discards. It never reads the request or a datasource.
func testContext() *gofr.Context {
	return &gofr.Context{
		Context:   context.Background(),
		Container: &container.Container{Logger: logging.NewFileLogger("")},
	}
}

// fixedDays and utc are the per-run settings, as plain functions.
func fixedDays(days int) func(*gofr.Context) int {
	return func(*gofr.Context) int { return days }
}

func utc(*gofr.Context) *time.Location { return time.UTC }

// seed puts one entry in the repository and returns its id.
func seed(t *testing.T, repo repository.TimesheetRepository, daysAgo int, status string) uint {
	t.Helper()

	entry := &model.Timesheet{
		UserID:        1,
		Date:          time.Now().In(time.UTC).AddDate(0, 0, -daysAgo),
		DurationHours: 4,
		Status:        status,
		Description:   &status,
	}

	created, err := repo.Save(context.Background(), entry)
	if err != nil {
		t.Fatalf("seed %s entry: %v", status, err)
	}

	return created.ID
}

func statusOf(t *testing.T, repo repository.TimesheetRepository, id uint) string {
	t.Helper()

	entry, err := repo.GetByID(context.Background(), id)
	if err != nil {
		t.Fatalf("read entry %d: %v", id, err)
	}

	return entry.Status
}

// The job's whole purpose, and its whole risk: old open entries move, recent
// ones do not.
func TestOnlyOpenEntriesPastTheCutoffAreSubmitted(t *testing.T) {
	repo := memory.NewTimesheetRepository()

	old := seed(t, repo, 30, model.TimesheetStatusOpen)
	recent := seed(t, repo, 1, model.TimesheetStatusOpen)

	worker.AutoSubmitStaleTimesheets(repo, fixedDays(14), utc)(testContext())

	if got := statusOf(t, repo, old); got != model.TimesheetStatusSubmitted {
		t.Errorf("the 30-day-old open entry is %q, want %q", got, model.TimesheetStatusSubmitted)
	}

	if got := statusOf(t, repo, recent); got != model.TimesheetStatusOpen {
		t.Errorf("yesterday's entry is %q; the sweep reached inside the cutoff", got)
	}
}

// Approving is a human decision and submitting is a state somebody already
// reached. Moving either would rewrite a record whose owner thought it was
// settled.
func TestEntriesThatAreNotOpenAreLeftAlone(t *testing.T) {
	repo := memory.NewTimesheetRepository()

	approved := seed(t, repo, 60, model.TimesheetStatusApproved)
	submitted := seed(t, repo, 60, model.TimesheetStatusSubmitted)

	worker.AutoSubmitStaleTimesheets(repo, fixedDays(7), utc)(testContext())

	if got := statusOf(t, repo, approved); got != model.TimesheetStatusApproved {
		t.Errorf("an approved entry became %q", got)
	}

	if got := statusOf(t, repo, submitted); got != model.TimesheetStatusSubmitted {
		t.Errorf("a submitted entry became %q", got)
	}
}

// The number of days is read on each run rather than captured when the job was
// registered, so the Settings screen can change it without a restart. Tested
// because "read per run" is invisible until it is wrong, and then it is wrong
// silently for as long as nobody restarts.
func TestTheCutoffIsReadOnEveryRun(t *testing.T) {
	repo := memory.NewTimesheetRepository()

	entry := seed(t, repo, 10, model.TimesheetStatusOpen)

	days := 30
	job := worker.AutoSubmitStaleTimesheets(repo, func(*gofr.Context) int { return days }, utc)

	job(testContext())

	if got := statusOf(t, repo, entry); got != model.TimesheetStatusOpen {
		t.Fatalf("a 10-day-old entry was submitted under a 30-day cutoff: %q", got)
	}

	// The administrator shortens it. The same job has to honour the new value.
	days = 7

	job(testContext())

	if got := statusOf(t, repo, entry); got != model.TimesheetStatusSubmitted {
		t.Errorf("the entry is %q; the shortened cutoff was not picked up", got)
	}
}

// failingRepository refuses to update one particular entry.
type failingRepository struct {
	repository.TimesheetRepository

	failFor uint
	calls   int
}

func (r *failingRepository) Update(
	ctx context.Context,
	entry *model.Timesheet,
) (*model.Timesheet, error) {
	r.calls++

	if entry.ID == r.failFor {
		return nil, errors.New("the row is locked")
	}

	return r.TimesheetRepository.Update(ctx, entry)
}

// One bad row must not abort the sweep. A job that stops at the first failure
// would leave the rest of the month unsubmitted, and nobody would look at the
// log of a cron job that ran at three in the morning and reported one error.
func TestOneFailingRowDoesNotAbortTheSweep(t *testing.T) {
	inner := memory.NewTimesheetRepository()

	first := seed(t, inner, 30, model.TimesheetStatusOpen)
	second := seed(t, inner, 31, model.TimesheetStatusOpen)
	third := seed(t, inner, 32, model.TimesheetStatusOpen)

	repo := &failingRepository{TimesheetRepository: inner, failFor: second}

	worker.AutoSubmitStaleTimesheets(repo, fixedDays(14), utc)(testContext())

	if repo.calls != 3 {
		t.Errorf("Update was called %d times, want 3 - the sweep stopped early", repo.calls)
	}

	for _, id := range []uint{first, third} {
		if got := statusOf(t, inner, id); got != model.TimesheetStatusSubmitted {
			t.Errorf("entry %d is %q; a neighbouring failure took it down too", id, got)
		}
	}

	if got := statusOf(t, inner, second); got != model.TimesheetStatusOpen {
		t.Errorf("the entry whose update failed is %q, want it unchanged", got)
	}
}

// erroringRepository cannot list anything.
type erroringRepository struct {
	repository.TimesheetRepository
}

func (erroringRepository) GetByFilter(
	_ context.Context,
	_ repository.TimesheetFilter,
) ([]*model.Timesheet, error) {
	return nil, errors.New("the database is unreachable")
}

// A cron job that panicked would take the process with it, and the process is
// also the web server everyone is using.
func TestAFailedListingIsReportedRatherThanFatal(t *testing.T) {
	repo := erroringRepository{TimesheetRepository: memory.NewTimesheetRepository()}

	// The assertion is that this returns at all.
	worker.AutoSubmitStaleTimesheets(repo, fixedDays(14), utc)(testContext())
}

// Nothing to do is the normal case on most nights, and it must not be an error.
func TestAnEmptySweepIsQuietAndHarmless(t *testing.T) {
	repo := memory.NewTimesheetRepository()

	worker.AutoSubmitStaleTimesheets(repo, fixedDays(14), utc)(testContext())

	entries, err := repo.GetByFilter(context.Background(), repository.TimesheetFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if len(entries) != 0 {
		t.Errorf("the sweep invented %d entries", len(entries))
	}
}
