package service

import (
	"context"
	"strings"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
	"github.com/dennis-dko/go-time-recording/internal/pkg/security"
)

// Credentials of the built-in administrator created on first start. The
// password is intentionally well known; the account is flagged
// MustChangePassword so both the API and the UI keep demanding a change.
const (
	SystemUserEmail    = "admin@local"
	SystemUserName     = "System Administrator"
	SystemUserPassword = "changeme123"
)

// Principal is an authenticated user together with the permissions their role
// currently grants.
type Principal struct {
	User        *model.User
	Role        *model.Role
	Permissions []string
}

// Can reports whether the principal holds the permission.
func (p *Principal) Can(permission string) bool {
	for _, granted := range p.Permissions {
		if granted == permission {
			return true
		}
	}

	return false
}

// AuthService authenticates users and resolves what they may do.
type AuthService struct {
	users repository.UserRepository
	roles repository.RoleRepository
}

// NewAuthService creates new instance.
func NewAuthService(users repository.UserRepository, roles repository.RoleRepository) *AuthService {
	return &AuthService{users: users, roles: roles}
}

// Authenticate verifies a login and returns the resulting principal.
//
// Every failure returns the same error regardless of cause, so the response
// cannot be used to discover which email addresses exist.
func (s *AuthService) Authenticate(ctx context.Context, email, password string) (*Principal, error) {
	user, err := s.users.GetByEmail(ctx, normalizeEmail(email))
	if err != nil {
		return nil, apperror.Invalidf("invalid credentials")
	}

	if user.PasswordHash == "" || !security.VerifyPassword(user.PasswordHash, password) {
		return nil, apperror.Invalidf("invalid credentials")
	}

	return s.principalFor(ctx, user)
}

// PrincipalByEmail resolves the caller behind an already-authenticated
// request. Authentication has happened by this point, so a missing user is a
// real inconsistency rather than a failed login.
func (s *AuthService) PrincipalByEmail(ctx context.Context, email string) (*Principal, error) {
	user, err := s.users.GetByEmail(ctx, normalizeEmail(email))
	if err != nil {
		return nil, err
	}

	return s.principalFor(ctx, user)
}

func (s *AuthService) principalFor(ctx context.Context, user *model.User) (*Principal, error) {
	role, err := s.roles.GetByID(ctx, user.RoleID)
	if err != nil {
		// A user pointing at a deleted role gets no permissions rather than
		// an error, so the rest of the system still treats them as a valid
		// but powerless account.
		return &Principal{User: user}, nil
	}

	return &Principal{User: user, Role: role, Permissions: role.Permissions}, nil
}

// EnsureSystemUser creates the built-in administrator if it is missing.
//
// It runs on every start rather than in a migration so an account that was
// deleted directly in the database is restored, and so the password is hashed
// by the same code that verifies it.
func (s *AuthService) EnsureSystemUser(ctx context.Context) (created bool, err error) {
	if _, err := s.users.GetByEmail(ctx, SystemUserEmail); err == nil {
		return false, nil
	}

	adminRole, err := s.roles.GetByName(ctx, model.RoleAdmin)
	if err != nil {
		return false, err
	}

	hash, err := security.HashPassword(SystemUserPassword)
	if err != nil {
		return false, apperror.Internal(err)
	}

	_, err = s.users.Save(ctx, &model.User{
		Name:               SystemUserName,
		Email:              SystemUserEmail,
		RoleID:             adminRole.ID,
		PasswordHash:       hash,
		MustChangePassword: true,
		IsSystem:           true,
		DailyTargetHours:   model.DefaultDailyTargetHours,
	})
	if err != nil {
		return false, err
	}

	return true, nil
}

// ChangePassword sets a new password for the user, clearing the
// must-change flag.
func (s *AuthService) ChangePassword(ctx context.Context, userID uint, current, next string) error {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return err
	}

	if !security.VerifyPassword(user.PasswordHash, current) {
		return apperror.Invalidf("the current password is not correct")
	}

	if current == next {
		return apperror.Invalidf("the new password must differ from the current one")
	}

	hash, err := security.HashPassword(next)
	if err != nil {
		return apperror.Invalidf("%v", err)
	}

	user.PasswordHash = hash
	user.MustChangePassword = false

	if _, err := s.users.Update(ctx, user); err != nil {
		return err
	}

	return nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
