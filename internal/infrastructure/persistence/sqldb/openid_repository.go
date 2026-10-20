package sqldb

import (
	"context"
	"strconv"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

const openIDColumns = "id, user_id, issuer, subject, created_at"

// OpenIDIdentityRepository keeps which account each provider identity is linked
// to, in a SQL database.
type OpenIDIdentityRepository struct {
	base
}

// NewOpenIDIdentityRepository creates an identity repository for the given dialect.
func NewOpenIDIdentityRepository(db DB, dialect string) *OpenIDIdentityRepository {
	return &OpenIDIdentityRepository{base{db: db, dialect: dialect}}
}

var _ repository.OpenIDIdentityRepository = (*OpenIDIdentityRepository)(nil)

func (r *OpenIDIdentityRepository) ForUser(ctx context.Context, userID uint) (*model.OpenIDIdentity, error) {
	row := r.db.QueryRowContext(ctx,
		r.rebind("SELECT "+openIDColumns+" FROM openid_identities WHERE user_id = ?"), userID)

	identity, err := scanOpenIDIdentity(row)
	if problem := problemReading(err, "identity", strconv.FormatUint(uint64(userID), 10)); problem != nil {
		return nil, problem
	}

	return identity, nil
}

func (r *OpenIDIdentityRepository) BySubject(ctx context.Context, issuer, subject string) (*model.OpenIDIdentity, error) {
	row := r.db.QueryRowContext(ctx,
		r.rebind("SELECT "+openIDColumns+" FROM openid_identities WHERE issuer = ? AND subject = ?"),
		issuer, subject)

	identity, err := scanOpenIDIdentity(row)
	if problem := problemReading(err, "identity", ""); problem != nil {
		return nil, problem
	}

	return identity, nil
}

// Link stores the identity over the one its account had - one statement in each
// engine's own words, as a calendar feed is replaced.
//
// The subject being linked to another account already is the caller's to have
// refused before this: the unique constraint on the pair is the backstop, and on
// MySQL ON DUPLICATE KEY UPDATE would meet that constraint by rewriting the other
// account's row with the values it already holds, which links nothing and says so
// nowhere.
func (r *OpenIDIdentityRepository) Link(ctx context.Context, identity *model.OpenIDIdentity) error {
	query := `INSERT INTO openid_identities (user_id, issuer, subject, created_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET issuer = EXCLUDED.issuer, subject = EXCLUDED.subject,
			created_at = EXCLUDED.created_at`

	if r.dialect == DialectMySQL {
		query = `INSERT INTO openid_identities (user_id, issuer, subject, created_at) VALUES (?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE issuer = VALUES(issuer), subject = VALUES(subject),
				created_at = VALUES(created_at)`
	}

	if _, err := r.exec(ctx, query, identity.UserID, identity.Issuer, identity.Subject, identity.CreatedAt); err != nil {
		return apperror.Internal(err)
	}

	return nil
}

func (r *OpenIDIdentityRepository) Unlink(ctx context.Context, userID uint) (bool, error) {
	affected, err := r.exec(ctx, "DELETE FROM openid_identities WHERE user_id = ?", userID)
	if err != nil {
		return false, apperror.Internal(err)
	}

	return affected > 0, nil
}

func scanOpenIDIdentity(s scanner) (*model.OpenIDIdentity, error) {
	var (
		identity  model.OpenIDIdentity
		createdAt dateTime
	)

	if err := s.Scan(&identity.ID, &identity.UserID, &identity.Issuer, &identity.Subject, &createdAt); err != nil {
		return nil, err
	}

	identity.CreatedAt = createdAt.Time

	return &identity, nil
}
