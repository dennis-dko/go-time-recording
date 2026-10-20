package sqldb

import (
	"context"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// WorkDayRepository keeps when each person worked, a day at a time.
type WorkDayRepository struct {
	base
}

// NewWorkDayRepository creates a working day repository for the given dialect.
func NewWorkDayRepository(db DB, dialect string) *WorkDayRepository {
	return &WorkDayRepository{base{db: db, dialect: dialect}}
}

var _ repository.WorkDayRepository = (*WorkDayRepository)(nil)

// Between returns somebody's recorded days from one day to another, earliest first.
func (r *WorkDayRepository) Between(ctx context.Context, userID uint, from, to time.Time) ([]model.WorkDay, error) {
	rows, err := r.db.QueryContext(ctx, r.rebind(
		"SELECT day, start_minute, end_minute, break_minutes FROM work_days "+
			"WHERE user_id = ? AND day >= ? AND day <= ? ORDER BY day"),
		userID, model.CalendarDay(from), model.CalendarDay(to))
	if err != nil {
		return nil, apperror.Internal(err)
	}

	defer func() { _ = rows.Close() }()

	var days []model.WorkDay

	for rows.Next() {
		recorded := model.WorkDay{UserID: userID}

		var day dateTime

		if err := rows.Scan(&day, &recorded.StartMinute, &recorded.EndMinute, &recorded.BreakMinutes); err != nil {
			return nil, apperror.Internal(err)
		}

		// A calendar day, whatever shape the driver handed it back in - the same
		// reading every stored date gets.
		recorded.Day = model.CalendarDay(day.Time)
		days = append(days, recorded)
	}

	if err := rows.Err(); err != nil {
		return nil, apperror.Internal(err)
	}

	return days, nil
}

// Save records a day, replacing what was recorded for it - one statement in each
// engine's own words, as an absence is saved.
func (r *WorkDayRepository) Save(ctx context.Context, day model.WorkDay) error {
	query := `INSERT INTO work_days (user_id, day, start_minute, end_minute, break_minutes) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (user_id, day) DO UPDATE SET start_minute = EXCLUDED.start_minute,
			end_minute = EXCLUDED.end_minute, break_minutes = EXCLUDED.break_minutes`

	if r.dialect == DialectMySQL {
		query = `INSERT INTO work_days (user_id, day, start_minute, end_minute, break_minutes) VALUES (?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE start_minute = VALUES(start_minute), end_minute = VALUES(end_minute),
				break_minutes = VALUES(break_minutes)`
	}

	if _, err := r.exec(ctx, query, day.UserID, model.CalendarDay(day.Day),
		day.StartMinute, day.EndMinute, day.BreakMinutes); err != nil {
		return apperror.Internal(err)
	}

	return nil
}

func (r *WorkDayRepository) Delete(ctx context.Context, userID uint, day time.Time) (bool, error) {
	affected, err := r.exec(ctx, "DELETE FROM work_days WHERE user_id = ? AND day = ?", userID, model.CalendarDay(day))
	if err != nil {
		return false, apperror.Internal(err)
	}

	return affected > 0, nil
}
