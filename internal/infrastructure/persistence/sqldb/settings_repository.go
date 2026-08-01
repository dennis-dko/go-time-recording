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
// An UPDATE-then-INSERT is used rather than dialect-specific upsert syntax
// (ON CONFLICT / ON DUPLICATE KEY), which differs across the engines.
func (r *SettingsRepository) Set(ctx context.Context, key, value string) error {
	affected, err := r.exec(ctx,
		"UPDATE settings SET value = ?, updated_at = ? WHERE key_name = ?", value, time.Now(), key)
	if err != nil {
		return apperror.Internal(err)
	}

	if affected > 0 {
		return nil
	}

	_, err = r.exec(ctx,
		"INSERT INTO settings (key_name, value, updated_at) VALUES (?, ?, ?)", key, value, time.Now())
	if err != nil {
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
