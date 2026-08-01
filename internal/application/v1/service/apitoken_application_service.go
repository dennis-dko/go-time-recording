package service

import (
	"context"
	"strings"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
	"github.com/dennis-dko/go-time-recording/internal/pkg/security"
)

// APITokenPrefix marks the value as a token of this application, so a leaked
// string is recognisable in a log or a repository scan.
const APITokenPrefix = "gtr_"

// maxTokensPerUser bounds how many tokens one account can accumulate.
const maxTokensPerUser = 20

// APITokenService issues and resolves personal API tokens.
type APITokenService struct {
	tokens repository.APITokenRepository
	users  repository.UserRepository
	auth   *AuthService
}

// NewAPITokenService creates new instance.
func NewAPITokenService(
	tokens repository.APITokenRepository,
	users repository.UserRepository,
	auth *AuthService,
) *APITokenService {
	return &APITokenService{tokens: tokens, users: users, auth: auth}
}

// IssuedToken is returned once, at creation. The secret is never retrievable
// again, which is why it is a separate type from the stored token.
type IssuedToken struct {
	Token *model.APIToken

	// Secret is the value the caller must keep; only its hash is stored.
	Secret string
}

// Create issues a token for the user.
//
// expiresInDays of 0 means the token does not expire on its own; it can still
// be revoked at any time.
func (s *APITokenService) Create(
	ctx context.Context,
	userID uint,
	name string,
	expiresInDays int,
) (*IssuedToken, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, apperror.InvalidFields("name")
	}

	if expiresInDays < 0 || expiresInDays > 3650 {
		return nil, apperror.InvalidFields("expiresInDays")
	}

	existing, err := s.tokens.ListForUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	if len(existing) >= maxTokensPerUser {
		return nil, apperror.Conflictf("at most %d tokens per user; revoke one first", maxTokensPerUser)
	}

	secret, err := security.NewSessionToken()
	if err != nil {
		return nil, apperror.Internal(err)
	}

	secret = APITokenPrefix + secret

	token := &model.APIToken{
		UserID:    userID,
		Name:      name,
		TokenHash: security.HashToken(secret),
		Prefix:    secret[:len(APITokenPrefix)+6],
		CreatedAt: time.Now(),
	}

	if expiresInDays > 0 {
		expiry := time.Now().AddDate(0, 0, expiresInDays)
		token.ExpiresAt = &expiry
	}

	saved, err := s.tokens.Save(ctx, token)
	if err != nil {
		return nil, err
	}

	return &IssuedToken{Token: saved, Secret: secret}, nil
}

// List returns the user's tokens, without any secret material.
func (s *APITokenService) List(ctx context.Context, userID uint) ([]*model.APIToken, error) {
	return s.tokens.ListForUser(ctx, userID)
}

// Revoke deletes one of the user's own tokens.
func (s *APITokenService) Revoke(ctx context.Context, id, userID uint) error {
	return s.tokens.Delete(ctx, id, userID)
}

// Resolve turns a presented token into its owner's principal.
//
// The permissions come from the owner's current role, evaluated now: a role
// change or a revoked permission takes effect on the very next request, and a
// token can never outrank the person it belongs to.
func (s *APITokenService) Resolve(ctx context.Context, secret string) (*Principal, error) {
	invalid := apperror.Invalidf("invalid token")

	if !strings.HasPrefix(secret, APITokenPrefix) {
		return nil, invalid
	}

	token, err := s.tokens.GetByHash(ctx, security.HashToken(secret))
	if err != nil {
		return nil, invalid
	}

	if token.Expired(time.Now()) {
		return nil, invalid
	}

	user, err := s.users.GetByID(ctx, token.UserID)
	if err != nil {
		return nil, invalid
	}

	// An account still on its initial password must not be usable through a
	// token either, or the password rule could simply be bypassed.
	if user.MustChangePassword {
		return nil, apperror.Conflictf("the account must change its initial password first")
	}

	// Best effort: failing to record usage must not fail the request.
	_ = s.tokens.TouchLastUsed(ctx, token.ID)

	return s.auth.principalFor(ctx, user)
}
