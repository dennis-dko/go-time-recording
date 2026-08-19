package sqldb

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/pkg/security"
)

// SealStoredSecrets encrypts what was written before a key was configured.
//
// Turning encryption on is something an installation does after it has been
// running, not before. There are already TOTP secrets in the users table and a
// bind password in the settings, written in the clear, and a key that applied
// only to what is written next would leave every one of them exactly as exposed
// as before while the configuration says otherwise. A second factor is enrolled
// once and then read for years; without this, "encryption is on" would be true of
// almost nothing.
//
// It reads the columns directly rather than going through the repositories,
// because that is the only way to see which values are already sealed: every read
// path decrypts, so a repository would hand back plaintext either way and this
// would rewrite the whole table on every start.
//
// Returns how many values it moved, which is zero on every start after the first.
func SealStoredSecrets(
	ctx context.Context,
	db DB,
	dialect string,
	secrets *security.Sealer,
) (int, error) {
	if !secrets.Enabled() {
		return 0, nil
	}

	moved, err := sealTOTPSecrets(ctx, db, dialect, secrets)
	if err != nil {
		return moved, err
	}

	sealedLDAP, err := sealLDAPPassword(ctx, db, dialect, secrets)
	if err != nil {
		return moved, err
	}

	return moved + sealedLDAP, nil
}

// sealTOTPSecrets encrypts the second factors that are still in the clear.
func sealTOTPSecrets(
	ctx context.Context,
	db DB,
	dialect string,
	secrets *security.Sealer,
) (int, error) {
	rows, err := db.QueryContext(ctx,
		Rebind(dialect, "SELECT id, totp_secret FROM users WHERE totp_secret <> ''"))
	if err != nil {
		return 0, fmt.Errorf("reading the stored second factors: %w", err)
	}

	// Collected before anything is written: an update while the rows are open is
	// a lock the same connection is waiting on, on SQLite.
	type pending struct {
		id     uint
		secret string
	}

	var plain []pending

	for rows.Next() {
		var row pending
		if err := rows.Scan(&row.id, &row.secret); err != nil {
			_ = rows.Close()

			return 0, fmt.Errorf("reading the stored second factors: %w", err)
		}

		if !security.IsSealed(row.secret) {
			plain = append(plain, row)
		}
	}

	err = rows.Err()
	_ = rows.Close()

	if err != nil {
		return 0, fmt.Errorf("reading the stored second factors: %w", err)
	}

	for _, row := range plain {
		sealed, sealErr := secrets.Seal(row.secret)
		if sealErr != nil {
			return 0, sealErr
		}

		if _, err := db.ExecContext(ctx,
			Rebind(dialect, "UPDATE users SET totp_secret = ? WHERE id = ?"),
			sealed, row.id); err != nil {
			return 0, fmt.Errorf("encrypting a stored second factor: %w", err)
		}
	}

	return len(plain), nil
}

// sealLDAPPassword encrypts the directory's bind password if it is still in the
// clear.
//
// Only that field. The rest of the configuration is a setting rather than a
// credential, and a stored setting is worth being able to read when something is
// wrong with it.
func sealLDAPPassword(
	ctx context.Context,
	db DB,
	dialect string,
	secrets *security.Sealer,
) (int, error) {
	var raw string

	err := db.QueryRowContext(ctx,
		Rebind(dialect, "SELECT value FROM settings WHERE key_name = ?"),
		model.SettingLDAPSettings).Scan(&raw)
	if err != nil || raw == "" {
		// No directory configured is the ordinary case, and sql.ErrNoRows says
		// exactly that. Nothing else here can fail in a way worth stopping a
		// start-up over.
		return 0, nil
	}

	var config model.LDAPConfig
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		// A corrupt entry is the settings screen's problem, not this one's.
		return 0, nil
	}

	if config.BindPassword == "" || security.IsSealed(config.BindPassword) {
		return 0, nil
	}

	sealed, err := secrets.Seal(config.BindPassword)
	if err != nil {
		return 0, err
	}

	config.BindPassword = sealed

	encoded, err := json.Marshal(config)
	if err != nil {
		return 0, err
	}

	if _, err := db.ExecContext(ctx,
		Rebind(dialect, "UPDATE settings SET value = ? WHERE key_name = ?"),
		string(encoded), model.SettingLDAPSettings); err != nil {
		return 0, fmt.Errorf("encrypting the directory password: %w", err)
	}

	return 1, nil
}
