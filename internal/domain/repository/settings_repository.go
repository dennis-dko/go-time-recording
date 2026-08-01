package repository

import "context"

// SettingsRepository stores the instance settings as key/value pairs.
type SettingsRepository interface {
	// Get returns the value, or an empty string when the key is unset. A
	// missing setting is a normal state and not reported as an error.
	Get(ctx context.Context, key string) (string, error)

	Set(ctx context.Context, key, value string) error

	GetAll(ctx context.Context) (map[string]string, error)
}
