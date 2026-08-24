package sqldb

import (
	"context"
	"strconv"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

const passkeyColumns = "id, user_id, name, credential_id, public_key, attestation_type, " +
	"transports, sign_count, backup_eligible, backed_up, created_at, last_used_at"

// PasskeyRepository stores WebAuthn credentials in a SQL database.
type PasskeyRepository struct {
	base
}

// NewPasskeyRepository creates a passkey repository for the given dialect.
func NewPasskeyRepository(db DB, dialect string) *PasskeyRepository {
	return &PasskeyRepository{base{db: db, dialect: dialect}}
}

var _ repository.PasskeyRepository = (*PasskeyRepository)(nil)

func (r *PasskeyRepository) Save(ctx context.Context, passkey *model.Passkey) (*model.Passkey, error) {
	id, err := r.insert(ctx,
		"INSERT INTO passkeys (user_id, name, credential_id, public_key, attestation_type, "+
			"transports, sign_count, backup_eligible, backed_up, created_at) "+
			"VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		passkey.UserID, passkey.Name, passkey.CredentialID, passkey.PublicKey,
		passkey.AttestationType, passkey.Transports, passkey.SignCount,
		passkey.BackupEligible, passkey.BackedUp, passkey.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			// The same authenticator was registered twice. Saying so is safe:
			// the caller is the owner, signed in, looking at their own list.
			return nil, apperror.Conflictf("this passkey is already registered").WithCode("passkeyKnown")
		}

		return nil, apperror.Internal(err)
	}

	created := *passkey
	created.ID = id

	return &created, nil
}

func (r *PasskeyRepository) GetByUser(ctx context.Context, userID uint) ([]*model.Passkey, error) {
	rows, err := r.db.QueryContext(ctx,
		r.rebind("SELECT "+passkeyColumns+" FROM passkeys WHERE user_id = ? ORDER BY id"), userID)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	defer func() { _ = rows.Close() }()

	var passkeys []*model.Passkey

	for rows.Next() {
		passkey, scanErr := scanPasskey(rows)
		if scanErr != nil {
			return nil, apperror.Internal(scanErr)
		}

		passkeys = append(passkeys, passkey)
	}

	if err := rows.Err(); err != nil {
		return nil, apperror.Internal(err)
	}

	return passkeys, nil
}

func (r *PasskeyRepository) GetByCredentialID(
	ctx context.Context,
	credentialID []byte,
) (*model.Passkey, error) {
	row := r.db.QueryRowContext(ctx,
		r.rebind("SELECT "+passkeyColumns+" FROM passkeys WHERE credential_id = ?"), credentialID)

	passkey, err := scanPasskey(row)
	if problem := problemReading(err, "passkey", "credential"); problem != nil {
		return nil, problem
	}

	return passkey, nil
}

// TouchUsage records a successful sign-in.
//
// A failure here is returned but the caller ignores it: the sign-in already
// succeeded, and refusing it afterwards because a timestamp could not be
// written would be the wrong trade.
func (r *PasskeyRepository) TouchUsage(ctx context.Context, id uint, signCount uint32) error {
	_, err := r.exec(ctx,
		"UPDATE passkeys SET last_used_at = ?, sign_count = ? WHERE id = ?",
		time.Now(), signCount, id)

	return err
}

func (r *PasskeyRepository) Delete(ctx context.Context, id, userID uint) error {
	// Scoped to the owner, so an id from somewhere else deletes nothing.
	affected, err := r.exec(ctx,
		"DELETE FROM passkeys WHERE id = ? AND user_id = ?", id, userID)
	if err != nil {
		return apperror.Internal(err)
	}

	if affected == 0 {
		return apperror.NotFound("passkey", strconv.FormatUint(uint64(id), 10))
	}

	return nil
}

func scanPasskey(s scanner) (*model.Passkey, error) {
	var (
		passkey  model.Passkey
		lastUsed dateTime
	)

	err := s.Scan(&passkey.ID, &passkey.UserID, &passkey.Name,
		&passkey.CredentialID, &passkey.PublicKey, &passkey.AttestationType,
		&passkey.Transports, &passkey.SignCount,
		&passkey.BackupEligible, &passkey.BackedUp,
		&passkey.CreatedAt, &lastUsed)
	if err != nil {
		return nil, err
	}

	if lastUsed.Valid {
		when := lastUsed.Time
		passkey.LastUsedAt = &when
	}

	return &passkey, nil
}
