package sqldb

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

// SettingsRepository stores the instance settings as key/value pairs.
type SettingsRepository struct {
	base
}

// NewSettingsRepository creates a settings repository for the given dialect.
func NewSettingsRepository(db DB, dialect string) *SettingsRepository {
	return &SettingsRepository{base{db: db, dialect: dialect}}
}

var _ repository.SettingsRepository = (*SettingsRepository)(nil)

// Get returns the stored value, or an empty string when the key is unset.
// A missing setting is a normal state, not an error.
func (r *SettingsRepository) Get(ctx context.Context, key string) (string, error) {
	var value string

	err := r.db.QueryRowContext(ctx,
		r.rebind("SELECT value FROM settings WHERE key_name = ?"), key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}

	if err != nil {
		return "", apperror.Internal(err)
	}

	return value, nil
}

// Set writes the value, replacing any previous one.
//
// A real upsert, in each engine's own words. It used to be an UPDATE and, if
// that reported nothing changed, an INSERT - which reads correctly and is wrong
// on MySQL, because MySQL counts *changed* rows rather than matched ones. Writing
// a value that is already there therefore reported nothing changed, fell through
// to the INSERT, and answered "Duplicate entry ... for key 'settings.PRIMARY'".
//
// Saving a form twice without editing it is not an unusual thing to do; it is
// what somebody does when they are not sure the first press registered. On MySQL
// that was a 500.
//
// The syntax differs by engine and there is no portable spelling, so this is the
// one place in the repositories that branches on the dialect. SQLite and
// PostgreSQL agree on the standard form.
func (r *SettingsRepository) Set(ctx context.Context, key, value string) error {
	query := `INSERT INTO settings (key_name, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT (key_name) DO UPDATE SET value = EXCLUDED.value,
			updated_at = EXCLUDED.updated_at`

	if r.dialect == "mysql" {
		query = `INSERT INTO settings (key_name, value, updated_at) VALUES (?, ?, ?)
			ON DUPLICATE KEY UPDATE value = VALUES(value), updated_at = VALUES(updated_at)`
	}

	if _, err := r.exec(ctx, query, key, value, time.Now()); err != nil {
		return apperror.Internal(err)
	}

	return nil
}

// GetAll returns every stored setting.
func (r *SettingsRepository) GetAll(ctx context.Context) (map[string]string, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT key_name, value FROM settings")
	if err != nil {
		return nil, apperror.Internal(err)
	}
	defer func() { _ = rows.Close() }()

	out := make(map[string]string)

	for rows.Next() {
		var key, value string
		if scanErr := rows.Scan(&key, &value); scanErr != nil {
			return nil, apperror.Internal(scanErr)
		}

		out[key] = value
	}

	if err := rows.Err(); err != nil {
		return nil, apperror.Internal(err)
	}

	return out, nil
}
