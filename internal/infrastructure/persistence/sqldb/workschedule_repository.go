package sqldb

import (
	"context"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// WorkScheduleRepository keeps the weeks people plan their hours in.
type WorkScheduleRepository struct {
	base
}

// NewWorkScheduleRepository creates a schedule repository for the given dialect.
func NewWorkScheduleRepository(db DB, dialect string) *WorkScheduleRepository {
	return &WorkScheduleRepository{base{db: db, dialect: dialect}}
}

var _ repository.WorkScheduleRepository = (*WorkScheduleRepository)(nil)

const workScheduleColumns = "valid_from, monday, tuesday, wednesday, thursday, friday, saturday, sunday"

// ForUser returns somebody's plans, earliest first.
func (r *WorkScheduleRepository) ForUser(ctx context.Context, userID uint) ([]model.WorkSchedule, error) {
	rows, err := r.db.QueryContext(ctx, r.rebind(
		"SELECT "+workScheduleColumns+" FROM work_schedules WHERE user_id = ? ORDER BY valid_from"),
		userID)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	defer func() { _ = rows.Close() }()

	var schedules []model.WorkSchedule

	for rows.Next() {
		schedule := model.WorkSchedule{UserID: userID}

		var from dateTime

		hours := &schedule.Hours

		if err := rows.Scan(&from, &hours[0], &hours[1], &hours[2], &hours[3],
			&hours[4], &hours[5], &hours[6]); err != nil {
			return nil, apperror.Internal(err)
		}

		// A calendar day, whatever shape the driver handed it back in - the same
		// reading every stored date gets.
		schedule.ValidFrom = model.CalendarDay(from.Time)
		schedules = append(schedules, schedule)
	}

	if err := rows.Err(); err != nil {
		return nil, apperror.Internal(err)
	}

	return schedules, nil
}

// Save records a plan, replacing the one that starts on the same day.
//
// One statement that replaces the row, in each engine's own words, as the timer
// does - two plans saved at once for the same day are then a question the
// database answers rather than a duplicate key for the second.
func (r *WorkScheduleRepository) Save(ctx context.Context, schedule model.WorkSchedule) error {
	query := `INSERT INTO work_schedules (user_id, ` + workScheduleColumns + `)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id, valid_from) DO UPDATE SET
			monday = EXCLUDED.monday, tuesday = EXCLUDED.tuesday,
			wednesday = EXCLUDED.wednesday, thursday = EXCLUDED.thursday,
			friday = EXCLUDED.friday, saturday = EXCLUDED.saturday, sunday = EXCLUDED.sunday`

	if r.dialect == DialectMySQL {
		query = `INSERT INTO work_schedules (user_id, ` + workScheduleColumns + `)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE
				monday = VALUES(monday), tuesday = VALUES(tuesday),
				wednesday = VALUES(wednesday), thursday = VALUES(thursday),
				friday = VALUES(friday), saturday = VALUES(saturday), sunday = VALUES(sunday)`
	}

	hours := schedule.Hours

	if _, err := r.exec(ctx, query, schedule.UserID, model.CalendarDay(schedule.ValidFrom),
		hours[0], hours[1], hours[2], hours[3], hours[4], hours[5], hours[6]); err != nil {
		return apperror.Internal(err)
	}

	return nil
}

// Delete removes the plan that starts on a day.
func (r *WorkScheduleRepository) Delete(ctx context.Context, userID uint, validFrom time.Time) (bool, error) {
	affected, err := r.exec(ctx, "DELETE FROM work_schedules WHERE user_id = ? AND valid_from = ?",
		userID, model.CalendarDay(validFrom))
	if err != nil {
		return false, apperror.Internal(err)
	}

	return affected > 0, nil
}
