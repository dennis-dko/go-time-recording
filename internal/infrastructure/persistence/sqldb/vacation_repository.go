package sqldb

import (
	"context"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// VacationRepository keeps how many days of leave a year gives each person.
type VacationRepository struct {
	base
}

// NewVacationRepository creates a vacation repository for the given dialect.
func NewVacationRepository(db DB, dialect string) *VacationRepository {
	return &VacationRepository{base{db: db, dialect: dialect}}
}

var _ repository.VacationRepository = (*VacationRepository)(nil)

// ForUser returns somebody's entitlements, earliest first.
func (r *VacationRepository) ForUser(ctx context.Context, userID uint) ([]model.VacationEntitlement, error) {
	rows, err := r.db.QueryContext(ctx, r.rebind(
		"SELECT from_year, days FROM vacation_entitlements WHERE user_id = ? ORDER BY from_year"), userID)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	defer func() { _ = rows.Close() }()

	var entitlements []model.VacationEntitlement

	for rows.Next() {
		entitlement := model.VacationEntitlement{UserID: userID}

		if err := rows.Scan(&entitlement.FromYear, &entitlement.Days); err != nil {
			return nil, apperror.Internal(err)
		}

		entitlements = append(entitlements, entitlement)
	}

	if err := rows.Err(); err != nil {
		return nil, apperror.Internal(err)
	}

	return entitlements, nil
}

// Save records an entitlement, replacing one from the same year - one statement
// in each engine's own words, as the planned week is saved.
func (r *VacationRepository) Save(ctx context.Context, entitlement model.VacationEntitlement) error {
	query := `INSERT INTO vacation_entitlements (user_id, from_year, days) VALUES (?, ?, ?)
		ON CONFLICT (user_id, from_year) DO UPDATE SET days = EXCLUDED.days`

	if r.dialect == DialectMySQL {
		query = `INSERT INTO vacation_entitlements (user_id, from_year, days) VALUES (?, ?, ?)
			ON DUPLICATE KEY UPDATE days = VALUES(days)`
	}

	if _, err := r.exec(ctx, query, entitlement.UserID, entitlement.FromYear, entitlement.Days); err != nil {
		return apperror.Internal(err)
	}

	return nil
}

// Delete removes the entitlement from a year.
func (r *VacationRepository) Delete(ctx context.Context, userID uint, fromYear int) (bool, error) {
	affected, err := r.exec(ctx, "DELETE FROM vacation_entitlements WHERE user_id = ? AND from_year = ?",
		userID, fromYear)
	if err != nil {
		return false, apperror.Internal(err)
	}

	return affected > 0, nil
}
