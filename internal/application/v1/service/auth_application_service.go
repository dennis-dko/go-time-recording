package service

import (
	"context"
	"errors"
	"slices"
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
	return slices.Contains(p.Permissions, permission)
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

// PrincipalByID resolves what an account may do right now, from the account
// alone.
//
// Deliberately not SessionService.Resolve, which answers the same question for a
// request and does one more thing on the way: it records the session as used.
// That is right for a request somebody made and wrong for a background check -
// the idle timeout exists to end a session nobody is using, and a check asking
// on behalf of a tab that is merely open would be the thing that stops it ever
// firing.
//
// So this reads the account and its role and nothing else. It says what the
// rights are, never that the session is still good; whoever calls it already
// knows which account it is asking about.
func (s *AuthService) PrincipalByID(ctx context.Context, id uint) (*Principal, error) {
	user, err := s.users.GetByID(ctx, id)
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
	if existing, err := s.users.GetByEmail(ctx, SystemUserEmail); err == nil {
		return false, s.repairSystemUser(ctx, existing)
	}

	adminRole, err := s.roles.GetByName(ctx, model.RoleAdmin)
	if err != nil {
		return false, err
	}

	hash, err := security.HashPassword(SystemUserPassword)
	if err != nil {
		return false, apperror.Internal(err)
	}

	// No daily target. It used to be seeded with the default eight, from when
	// this account recorded time like anybody else - it does not: it cannot book
	// an hour, read a figure or open the working-times card, which is hidden for
	// it. So the figure was read by nothing and shown in one place, the account
	// table, where every other row said "default" and this one said 8.0 for no
	// reason a reader could work out.
	_, err = s.users.Save(ctx, &model.User{
		Name:               SystemUserName,
		Email:              SystemUserEmail,
		RoleID:             adminRole.ID,
		PasswordHash:       hash,
		MustChangePassword: true,
		IsSystem:           true,
	})
	if err != nil {
		return false, err
	}

	return true, nil
}

// repairSystemUser restores the two properties the built-in administrator is
// defined by, in case a directory sync, a migration or a hand-edited database
// row ever took them away.
//
// It runs on every start because this account is the guaranteed way back into
// an installation, and an account the directory owns would not be that.
func (s *AuthService) repairSystemUser(ctx context.Context, user *model.User) error {
	if user.IsSystem && !user.IsExternal && user.ExternalID == "" {
		return nil
	}

	user.IsSystem = true
	user.IsExternal = false
	user.ExternalID = ""

	_, err := s.users.Update(ctx, user)

	return err
}

// ChangePassword sets a new password for the user, clearing the
// must-change flag.
func (s *AuthService) ChangePassword(ctx context.Context, userID uint, current, next string) error {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return err
	}

	if !security.VerifyPassword(user.PasswordHash, current) {
		return apperror.Invalidf("the current password is not correct").WithCode("wrongCurrentPassword")
	}

	if current == next {
		return apperror.Invalidf("the new password must differ from the current one").
			WithCode("passwordUnchanged")
	}

	hash, err := security.HashPassword(next)
	if err != nil {
		return passwordError(err)
	}

	user.PasswordHash = hash
	user.MustChangePassword = false

	if _, err := s.users.Update(ctx, user); err != nil {
		return err
	}

	return nil
}

// SetPassword gives an account a password without asking for the one it had.
//
// For an administrator letting somebody back in who has forgotten theirs. That
// was impossible: a password is set at creation and changed by its owner through
// ChangePassword, which asks for the current one, so the only remaining move was
// to delete the account - taking every hour recorded in it - and make it again.
// The choice was somebody's time or somebody's access.
//
// The account is flagged to change it, exactly as a new one is, so what the
// administrator typed does not stay standing as a password a second person once
// knew. Note what that flag does and does not do: it stops the account using the
// application, and it deliberately does not stop signing in or setting a new
// password - it cannot, or nobody could ever get out of it.
//
// Refused for a directory account. The password of one lives in the directory
// and is checked against it; the local hash is only consulted for an account
// that is not external, so writing one here would store something that can never
// be used and report success for a reset that did not happen.
//
// Whether the caller may do this at all, and whether it is their own account, is
// the interface's question rather than this one's - this is handed a user id and
// a password and says whether the account can take one.
func (s *AuthService) SetPassword(ctx context.Context, userID uint, next string) error {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return err
	}

	if user.IsExternal {
		return apperror.Conflictf(
			"%q comes from the directory; its password is kept there and is checked "+
				"against it, so one set here would never be used", user.Email).
			WithCode("directoryAccountReadOnly", user.Email)
	}

	hash, err := security.HashPassword(next)
	if err != nil {
		return passwordError(err)
	}

	user.PasswordHash = hash
	user.MustChangePassword = true

	if _, err := s.users.Update(ctx, user); err != nil {
		return err
	}

	return nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// passwordError names the one failure hashing a password reports that the person
// typing it can act on.
//
// It reached them as bare prose from a package that knows nothing about who is
// reading - "password must be at least 12 characters", in English, whatever
// language they had chosen. Everything else out of bcrypt is a fault rather than
// an instruction, and stays as it was.
func passwordError(err error) *apperror.Error {
	if errors.Is(err, security.ErrPasswordTooShort) {
		return apperror.Invalidf("%v", err).
			WithCode("passwordTooShort", security.MinPasswordLength)
	}

	// The other end, and the one that used to escape as bcrypt's own sentence.
	// The limit is in bytes, so the message says characters and the translation
	// says why a German passphrase reaches it sooner.
	if errors.Is(err, security.ErrPasswordTooLong) {
		return apperror.Invalidf("%v", err).
			WithCode("passwordTooLong", security.MaxPasswordLength)
	}

	return apperror.Invalidf("%v", err)
}
