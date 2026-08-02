package sqldb

import (
	"context"
	"strconv"

	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

// PurgeUser removes a user and everything that references them.
//
// The order follows the foreign keys: time entries first, then the projects
// the user privately owned, then credentials, then the account itself.
//
// This is irreversible and takes the person's recorded hours with it. It
// exists for directory synchronisation, where an account that disappeared
// upstream must not linger here.
//
// The steps are not wrapped in a transaction, and deliberately so: each one is
// idempotent, so a failure part-way through is simply repeated by the next
// synchronisation run until the account is gone. Reaching for a transaction
// here would mean tying this package to one datasource's concrete Tx type for
// a job that is retried anyway.
func (r *UserRepository) PurgeUser(ctx context.Context, userID uint) error {
	steps := []struct {
		what  string
		query string
	}{
		{"time entries", "DELETE FROM timesheets WHERE user_id = ?"},
		// Only private projects go. A shared project has no owner and belongs
		// to the installation, not to the person who happened to create it.
		{"private projects", "DELETE FROM projects WHERE owner_id = ?"},
		{"api tokens", "DELETE FROM api_tokens WHERE user_id = ?"},
		{"sessions", "DELETE FROM sessions WHERE user_id = ?"},
	}

	for _, step := range steps {
		if _, err := r.exec(ctx, step.query, userID); err != nil {
			return apperror.Internal(err)
		}
	}

	affected, err := r.exec(ctx, "DELETE FROM users WHERE id = ?", userID)
	if err != nil {
		return apperror.Internal(err)
	}

	if affected == 0 {
		return apperror.NotFound("user", strconv.FormatUint(uint64(userID), 10))
	}

	return nil
}
