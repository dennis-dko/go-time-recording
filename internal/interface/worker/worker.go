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
//
// timezone reports the instance zone, read on each run so an administrator
// changing it does not have to restart. The cutoff is a calendar day, and a
// job running at 03:00 server time would otherwise count a different day than
// the people whose entries it is submitting.
// afterDays is likewise read per run rather than captured, so the Settings
// screen can change how long an entry may stay open without a restart.
func AutoSubmitStaleTimesheets(
	timesheets repository.TimesheetRepository,
	afterDays func(ctx *gofr.Context) int,
	timezone func(ctx *gofr.Context) *time.Location,
) gofr.CronFunc {
	return func(ctx *gofr.Context) {
		days := afterDays(ctx)
		cutoff := time.Now().In(timezone(ctx)).AddDate(0, 0, -days)

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
			ctx.Logger.Infof("auto-submit: submitted %d timesheet(s) older than %d days", submitted, days)
		}
	}
}
