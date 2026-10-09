package main

import (
	"context"

	"gofr.dev/pkg/gofr"

	appservice "github.com/dennis-dko/go-time-recording/internal/application/v1/service"
)

// synchroniser is as much of the directory synchronisation as its schedule uses.
type synchroniser interface {
	Sync(ctx context.Context) (*appservice.SyncReport, error)
}

// scheduledSync is the job the directory schedule runs.
//
// Ended when the process is told to stop, as well as by its own work. GoFr
// starts a job on a context nothing cancels, so a run still going at a stop went
// on deleting until the shutdown's deadline, the database was then closed under
// it, and if it was still going at that moment the lines naming what it had
// removed - the only record of a deletion - raced the end of the process. Ended
// at the stop, it stops at its next call to the database, and the report it
// hands back says what it did.
//
// What each run came to is counted in runs as well. GoFr counts a job as a
// success whenever it returns, and a run refused by a guard or cut off from the
// directory returns like one that worked - and nobody presses the button for a
// scheduled run, so nobody sees the refusal the screen would have shown.
func scheduledSync(stopping context.Context, directory synchroniser, runs appservice.Recorder) gofr.CronFunc {
	return func(ctx *gofr.Context) {
		run, cancel := context.WithCancel(ctx)
		defer cancel()

		unhook := context.AfterFunc(stopping, cancel)
		defer unhook()

		report, err := directory.Sync(run)

		// What it did, before whether it finished. A run that removed three
		// accounts and then lost the database used to log "directory sync
		// failed" and nothing else - three people's recorded hours gone, and
		// no line naming them. The deletions are logged from the report
		// whichever way the run ended, in the report's own words.
		for _, removed := range report.Removals() {
			ctx.Logger.Warn(removed)
		}

		if err != nil {
			ctx.Logger.Errorf("directory sync failed: %v", err)
			countRun(ctx, runs, appservice.DirectoryRunFailed)

			return
		}

		if report.Aborted != "" {
			ctx.Logger.Warnf("directory sync refused: %s", report.Aborted)
			countRun(ctx, runs, appservice.DirectoryRunRefused)

			return
		}

		if len(report.Created) > 0 {
			ctx.Logger.Infof("directory sync added %d account(s)", len(report.Created))
		}

		countRun(ctx, runs, appservice.DirectoryRunCompleted)
	}
}

// countRun records what a scheduled run came to, where there is anything to
// record it in.
func countRun(ctx context.Context, runs appservice.Recorder, outcome string) {
	if runs != nil {
		runs.IncrementCounter(ctx, appservice.MetricDirectoryRuns, "outcome", outcome)
	}
}
