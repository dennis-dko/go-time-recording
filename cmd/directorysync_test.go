package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"
	"gofr.dev/pkg/gofr/logging"
	"gofr.dev/pkg/gofr/testutil"

	appservice "github.com/dennis-dko/go-time-recording/internal/application/v1/service"
)

// slowRun has removed one account and works on until it is told to stop, which
// is how a run over a large directory looks to a process stopping under it.
type slowRun struct{}

func (slowRun) Sync(ctx context.Context) (*appservice.SyncReport, error) {
	report := &appservice.SyncReport{Deleted: []appservice.SyncCandidate{
		{Email: "left@example.com", Timesheets: 3},
	}}

	<-ctx.Done()

	return report, ctx.Err()
}

// A scheduled run stops when the process is told to stop, and says what it
// removed before it does.
//
// GoFr starts a job on a context nothing cancels. A run still going when the
// process was told to stop went on until the shutdown's deadline, the database
// was then closed under it, and the lines naming what it had removed - the only
// record of a deletion - raced the end of the process.
func TestAScheduledRunStopsWithTheProcessAndSaysWhatItRemoved(t *testing.T) {
	stopping, stop := context.WithCancel(context.Background())
	defer stop()

	out := testutil.StdoutOutputForFunc(func() {
		ctx := &gofr.Context{
			Context:   context.Background(),
			Container: &container.Container{Logger: logging.NewMockLogger(logging.INFO)},
		}

		finished := make(chan struct{})

		go func() {
			defer close(finished)

			scheduledSync(stopping, slowRun{}, nil)(ctx)
		}()

		stop()

		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("the scheduled run went on after the process was told to stop")
		}
	})

	if !strings.Contains(out, `directory sync removed "left@example.com"`) {
		t.Errorf("the run that was stopped did not say whom it had removed: %q", out)
	}
}

// fixedRun ends the way it was told to.
type fixedRun struct {
	report *appservice.SyncReport
	err    error
}

func (r fixedRun) Sync(context.Context) (*appservice.SyncReport, error) { return r.report, r.err }

// runCounter keeps the outcome of every run it is told about.
type runCounter struct{ outcomes []string }

func (c *runCounter) IncrementCounter(_ context.Context, name string, labels ...string) {
	if name == appservice.MetricDirectoryRuns {
		c.outcomes = append(c.outcomes, strings.Join(labels, "="))
	}
}

func (*runCounter) RecordHistogram(context.Context, string, float64, ...string) {}

// A scheduled run says what it came to where something other than the log can
// see it.
//
// GoFr counts a job in app_cron_job_success when it returns, and a run that a
// guard refused or that lost the directory returns like one that worked - so a
// schedule that had refused every night for a month looked, on every dashboard,
// like a month of reconciliation. Nobody presses the button for a scheduled
// run, so nobody sees the refusal the screen would have shown.
func TestAScheduledRunIsCountedByWhatItCameTo(t *testing.T) {
	counter := &runCounter{}

	for _, run := range []fixedRun{
		{report: &appservice.SyncReport{Aborted: "the directory answered with nobody"}},
		{report: &appservice.SyncReport{}, err: errors.New("the directory did not answer")},
		{report: &appservice.SyncReport{}},
	} {
		testutil.StdoutOutputForFunc(func() {
			ctx := &gofr.Context{
				Context:   context.Background(),
				Container: &container.Container{Logger: logging.NewMockLogger(logging.INFO)},
			}

			scheduledSync(context.Background(), run, counter)(ctx)
		})
	}

	want := []string{"outcome=refused", "outcome=failed", "outcome=completed"}
	if strings.Join(counter.outcomes, " ") != strings.Join(want, " ") {
		t.Errorf("the runs were counted as %v, want %v", counter.outcomes, want)
	}
}
