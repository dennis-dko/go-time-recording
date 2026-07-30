// Package worker holds the scheduled background jobs. They are an entry point
// into the application just like the HTTP handlers, which is why they live
// beside them in the interface layer rather than inside the domain.
package worker

import (
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
)

// AutoSubmitStaleTimesheets returns a cron job that submits time entries left
// open for longer than afterDays, so forgotten entries still reach approval.
//
// The job only moves open -> submitted; approving remains a human decision.
func AutoSubmitStaleTimesheets(
	timesheets repository.TimesheetRepository,
	afterDays int,
) gofr.CronFunc {
	return func(ctx *gofr.Context) {
		cutoff := time.Now().AddDate(0, 0, -afterDays)

		stale, err := timesheets.GetByFilter(ctx, repository.TimesheetFilter{
			Status:  model.TimesheetStatusOpen,
			EndDate: &cutoff,
		})
		if err != nil {
			ctx.Logger.Errorf("auto-submit: could not load stale timesheets: %v", err)

			return
		}

		var submitted int

		for _, entry := range stale {
			entry.Status = model.TimesheetStatusSubmitted

			if _, err := timesheets.Update(ctx, entry); err != nil {
				// One bad row should not abort the sweep.
				ctx.Logger.Errorf("auto-submit: could not submit timesheet %d: %v", entry.ID, err)

				continue
			}

			submitted++
		}

		if submitted > 0 {
			ctx.Logger.Infof("auto-submit: submitted %d timesheet(s) older than %d days", submitted, afterDays)
		}
	}
}
