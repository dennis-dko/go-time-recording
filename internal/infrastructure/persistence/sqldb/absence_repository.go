package sqldb

import (
	"context"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// AbsenceRepository keeps the days people record themselves as away.
type AbsenceRepository struct {
	base
}

// NewAbsenceRepository creates an absence repository for the given dialect.
func NewAbsenceRepository(db DB, dialect string) *AbsenceRepository {
	return &AbsenceRepository{base{db: db, dialect: dialect}}
}

var _ repository.AbsenceRepository = (*AbsenceRepository)(nil)

// Between returns somebody's absences from one day to another, earliest first.
func (r *AbsenceRepository) Between(
	ctx context.Context,
	userID uint,
	from, to time.Time,
) ([]model.Absence, error) {
	rows, err := r.db.QueryContext(ctx, r.rebind(
		"SELECT day, kind FROM absences WHERE user_id = ? AND day >= ? AND day <= ? ORDER BY day"),
		userID, model.CalendarDay(from), model.CalendarDay(to))
	if err != nil {
		return nil, apperror.Internal(err)
	}

	defer func() { _ = rows.Close() }()

	var absences []model.Absence

	for rows.Next() {
		absence := model.Absence{UserID: userID}

		var day dateTime

		if err := rows.Scan(&day, &absence.Kind); err != nil {
			return nil, apperror.Internal(err)
		}

		// A calendar day, whatever shape the driver handed it back in - the same
		// reading every stored date gets.
		absence.Day = model.CalendarDay(day.Time)
		absences = append(absences, absence)
	}

	if err := rows.Err(); err != nil {
		return nil, apperror.Internal(err)
	}

	return absences, nil
}

// Save records absences, each replacing whatever was on its day, in one
// transaction: a fortnight's leave that landed half way would leave somebody
// looking at a week of it.
func (r *AbsenceRepository) Save(ctx context.Context, absences []model.Absence) error {
	if len(absences) == 0 {
		return nil
	}

	query := `INSERT INTO absences (user_id, day, kind) VALUES (?, ?, ?)
		ON CONFLICT (user_id, day) DO UPDATE SET kind = EXCLUDED.kind`

	if r.dialect == DialectMySQL {
		query = `INSERT INTO absences (user_id, day, kind) VALUES (?, ?, ?)
			ON DUPLICATE KEY UPDATE kind = VALUES(kind)`
	}

	return r.withTx(ctx, func(tx base) error {
		for _, absence := range absences {
			if _, err := tx.db.ExecContext(ctx, tx.rebind(query),
				absence.UserID, model.CalendarDay(absence.Day), absence.Kind); err != nil {
				return apperror.Internal(err)
			}
		}

		return nil
	})
}

// Delete removes the absence recorded on a day.
func (r *AbsenceRepository) Delete(ctx context.Context, userID uint, day time.Time) (bool, error) {
	affected, err := r.exec(ctx, "DELETE FROM absences WHERE user_id = ? AND day = ?",
		userID, model.CalendarDay(day))
	if err != nil {
		return false, apperror.Internal(err)
	}

	return affected > 0, nil
}
