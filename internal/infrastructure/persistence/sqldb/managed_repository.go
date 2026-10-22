package sqldb

import (
	"context"

	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// ManagedAccountRepository keeps the managers' assignments in a SQL database.
type ManagedAccountRepository struct {
	base
}

// NewManagedAccountRepository reads and writes the assignments of the given database.
func NewManagedAccountRepository(db DB, dialect string) *ManagedAccountRepository {
	return &ManagedAccountRepository{base{db: db, dialect: dialect}}
}

var _ repository.ManagedAccountRepository = (*ManagedAccountRepository)(nil)

func (r *ManagedAccountRepository) ManagersOf(ctx context.Context, userID uint) ([]uint, error) {
	return r.ids(ctx, "SELECT manager_id FROM managed_accounts WHERE user_id = ? ORDER BY manager_id", userID)
}

func (r *ManagedAccountRepository) MembersOf(ctx context.Context, managerID uint) ([]uint, error) {
	return r.ids(ctx, "SELECT user_id FROM managed_accounts WHERE manager_id = ? ORDER BY user_id", managerID)
}

// SetManagers replaces an account's managers in one transaction, so a failure part
// way leaves the ones it had rather than some of each.
func (r *ManagedAccountRepository) SetManagers(ctx context.Context, userID uint, managerIDs []uint) error {
	err := r.withTx(ctx, func(tx base) error {
		if _, err := tx.exec(ctx, "DELETE FROM managed_accounts WHERE user_id = ?", userID); err != nil {
			return err
		}

		for _, managerID := range managerIDs {
			if _, err := tx.exec(ctx,
				"INSERT INTO managed_accounts (manager_id, user_id) VALUES (?, ?)", managerID, userID); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return apperror.Internal(err)
	}

	return nil
}

// ids reads a column of ids.
func (r *ManagedAccountRepository) ids(ctx context.Context, query string, id uint) ([]uint, error) {
	rows, err := r.db.QueryContext(ctx, r.rebind(query), id)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	defer func() { _ = rows.Close() }()

	ids := []uint{}

	for rows.Next() {
		var read uint

		if err := rows.Scan(&read); err != nil {
			return nil, apperror.Internal(err)
		}

		ids = append(ids, read)
	}

	if err := rows.Err(); err != nil {
		return nil, apperror.Internal(err)
	}

	return ids, nil
}
