package main

import (
	"context"
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

			scheduledSync(stopping, slowRun{})(ctx)
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
