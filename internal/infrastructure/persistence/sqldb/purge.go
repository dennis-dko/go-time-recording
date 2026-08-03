package sqldb

import (
	"context"
	"fmt"
	"strconv"

	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

// PurgeUser removes a user and everything that references them.
//
// The order follows the foreign keys: time entries first, then the projects the
// user privately owned, then credentials, then the account itself.
//
// This is irreversible and takes the person's recorded hours with it. It is used
// by directory synchronisation, where an account that disappeared upstream must
// not linger here, and by an administrator deleting an account deliberately -
// which asks for confirmation first, because this is the operation that destroys
// the hours.
//
// # Why it is a transaction
//
// It did not used to be, on the grounds that every step is idempotent and the
// synchronisation would repeat a failed purge on its next run. That reasoning
// does not survive the failure it was meant to cover.
//
// The final DELETE is the one that can be refused: a database that enforces the
// foreign keys rejects it while anything still points at the account. By then the
// time entries are already gone. So the account remains, its recorded hours do
// not, and repeating the purge cannot bring them back. If the reason is permanent
// - a table this list forgot, which is exactly what happened with passkeys - the
// state is permanent too.
//
// Inside a transaction the same failure leaves the account and its hours intact,
// and the error says what stopped it.
func (r *UserRepository) PurgeUser(ctx context.Context, userID uint) error {
	return r.withTx(ctx, func(tx base) error {
		// Every table with a foreign key to users has to appear here. One that is
		// missing does not fail quietly: on PostgreSQL and MySQL it makes the
		// final delete impossible, and on SQLite - where foreign keys are not
		// enforced unless asked for - it leaves rows pointing at an account that
		// no longer exists.
		steps := []struct {
			what  string
			query string
		}{
			{"time entries", "DELETE FROM timesheets WHERE user_id = ?"},
			// Only private projects go. A shared project has no owner and belongs
			// to the installation, not to the person who happened to create it.
			{"private projects", "DELETE FROM projects WHERE owner_id = ?"},
			{"api tokens", "DELETE FROM api_tokens WHERE user_id = ?"},
			{"passkeys", "DELETE FROM passkeys WHERE user_id = ?"},
			{"sessions", "DELETE FROM sessions WHERE user_id = ?"},
		}

		for _, step := range steps {
			if _, err := tx.exec(ctx, step.query, userID); err != nil {
				return apperror.Internal(fmt.Errorf("removing the %s of user %d: %w", step.what, userID, err))
			}
		}

		affected, err := tx.exec(ctx, "DELETE FROM users WHERE id = ?", userID)
		if err != nil {
			return apperror.Internal(err)
		}

		if affected == 0 {
			return apperror.NotFound("user", strconv.FormatUint(uint64(userID), 10))
		}

		return nil
	})
}
