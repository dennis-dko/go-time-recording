package service

import (
	"context"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
	"github.com/dennis-dko/go-time-recording/internal/pkg/security"
)

// ErrTOTPRequired tells the caller that the password was right but a
// second factor is still missing, so the UI can ask for the code instead of
// reporting a failed sign-in.
var ErrTOTPRequired = apperror.Conflictf("a two-factor code is required")

// ExternalAuthenticator checks credentials against a directory such as LDAP.
// It is an interface so the session service does not depend on the LDAP
// client, and so tests can substitute it.
type ExternalAuthenticator interface {
	// Enabled reports whether an external directory is configured at all.
	Enabled() bool

	// Authenticate verifies the credentials and returns the directory's view
	// of the user. A false result means "not this directory's user", which
	// lets the caller fall back to a local password.
	Authenticate(ctx context.Context, email, password string) (*ExternalUser, bool, error)
}

// ExternalUser is what a directory knows about someone.
type ExternalUser struct {
	Email string
	Name  string
}

// SessionService signs users in and out.
type SessionService struct {
	users    repository.UserRepository
	roles    repository.RoleRepository
	sessions repository.SessionRepository
	auth     *AuthService

	external ExternalAuthenticator

	lifetime time.Duration

	// defaultRole is given to accounts provisioned from the directory.
	defaultRole string
}

// NewSessionService creates new instance.
func NewSessionService(
	users repository.UserRepository,
	roles repository.RoleRepository,
	sessions repository.SessionRepository,
	auth *AuthService,
	lifetime time.Duration,
) *SessionService {
	return &SessionService{
		users:       users,
		roles:       roles,
		sessions:    sessions,
		auth:        auth,
		lifetime:    lifetime,
		defaultRole: model.RoleEmployee,
	}
}

// WithExternalAuth attaches a directory to authenticate against.
func (s *SessionService) WithExternalAuth(external ExternalAuthenticator, defaultRole string) *SessionService {
	s.external = external

	if defaultRole != "" {
		s.defaultRole = defaultRole
	}

	return s
}

// LoginResult carries the token to hand to the client.
type LoginResult struct {
	Token     string
	ExpiresAt time.Time
	Principal *Principal
}

// Login verifies credentials and opens a session.
//
// A wrong password and an unknown account fail identically, so the response
// cannot be used to discover which addresses exist. A missing second factor is
// the one distinguishable case, because the client has to be told to ask for
// the code.
func (s *SessionService) Login(ctx context.Context, email, password, totpCode string) (*LoginResult, error) {
	user, err := s.resolveUser(ctx, email, password)
	if err != nil {
		return nil, err
	}

	if user.TOTPEnabled {
		if totpCode == "" {
			return nil, ErrTOTPRequired
		}

		if !security.VerifyTOTP(user.TOTPSecret, totpCode) {
			return nil, apperror.Invalidf("the two-factor code is not valid")
		}
	}

	token, err := security.NewSessionToken()
	if err != nil {
		return nil, apperror.Internal(err)
	}

	now := time.Now()
	session := &model.Session{
		TokenHash: security.HashToken(token),
		UserID:    user.ID,
		CreatedAt: now,
		ExpiresAt: now.Add(s.lifetime),
	}

	if err := s.sessions.Save(ctx, session); err != nil {
		return nil, err
	}

	principal, err := s.auth.principalFor(ctx, user)
	if err != nil {
		return nil, err
	}

	return &LoginResult{Token: token, ExpiresAt: session.ExpiresAt, Principal: principal}, nil
}

// resolveUser finds the account behind the credentials, consulting the
// directory first when one is configured.
func (s *SessionService) resolveUser(ctx context.Context, email, password string) (*model.User, error) {
	invalid := apperror.Invalidf("invalid credentials")

	if s.external != nil && s.external.Enabled() {
		directoryUser, ok, err := s.external.Authenticate(ctx, email, password)
		if err != nil {
			return nil, apperror.Internal(err)
		}

		if ok {
			return s.provisionExternal(ctx, directoryUser)
		}
	}

	user, err := s.users.GetByEmail(ctx, normalizeEmail(email))
	if err != nil {
		return nil, invalid
	}

	// An account backed by the directory has no usable local password, so it
	// must not fall through to a local check.
	if user.IsExternal {
		return nil, invalid
	}

	if user.PasswordHash == "" || !security.VerifyPassword(user.PasswordHash, password) {
		return nil, invalid
	}

	return user, nil
}

// provisionExternal returns the local account for a directory user, creating
// it on first sign-in.
func (s *SessionService) provisionExternal(ctx context.Context, directoryUser *ExternalUser) (*model.User, error) {
	email := normalizeEmail(directoryUser.Email)

	existing, err := s.users.GetByEmail(ctx, email)
	if err == nil {
		return existing, nil
	}

	role, err := s.roles.GetByName(ctx, s.defaultRole)
	if err != nil {
		return nil, err
	}

	name := directoryUser.Name
	if name == "" {
		name = email
	}

	return s.users.Save(ctx, &model.User{
		Name:             name,
		Email:            email,
		RoleID:           role.ID,
		IsExternal:       true,
		DailyTargetHours: model.DefaultDailyTargetHours,
	})
}

// Resolve turns a session token from a cookie into its principal.
func (s *SessionService) Resolve(ctx context.Context, token string) (*Principal, error) {
	if token == "" {
		return nil, apperror.Invalidf("no session")
	}

	session, err := s.sessions.Get(ctx, security.HashToken(token))
	if err != nil {
		return nil, apperror.Invalidf("no session")
	}

	if session.Expired(time.Now()) {
		// Clean up on the way past, so expired rows do not need the sweep to
		// disappear the moment someone tries to use them.
		_ = s.sessions.Delete(ctx, session.TokenHash)

		return nil, apperror.Invalidf("session expired")
	}

	user, err := s.users.GetByID(ctx, session.UserID)
	if err != nil {
		return nil, apperror.Invalidf("no session")
	}

	return s.auth.principalFor(ctx, user)
}

// Logout ends one session.
func (s *SessionService) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}

	return s.sessions.Delete(ctx, security.HashToken(token))
}

// LogoutAll ends every session of a user, used after a password change or
// when their rights are altered.
func (s *SessionService) LogoutAll(ctx context.Context, userID uint) error {
	return s.sessions.DeleteForUser(ctx, userID)
}

// PruneExpired removes timed-out sessions.
func (s *SessionService) PruneExpired(ctx context.Context) (int64, error) {
	return s.sessions.DeleteExpired(ctx)
}

// BeginTOTPEnrolment generates a secret and returns what the user needs to add
// the account to an authenticator app.
//
// The secret is stored but not activated: only ConfirmTOTP switches it on,
// after the user has proven they can produce a code from it.
func (s *SessionService) BeginTOTPEnrolment(
	ctx context.Context,
	userID uint,
	issuer string,
) (secret, uri string, err error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return "", "", err
	}

	if user.TOTPEnabled {
		return "", "", apperror.Conflictf("two-factor authentication is already enabled")
	}

	secret, err = security.NewTOTPSecret()
	if err != nil {
		return "", "", apperror.Internal(err)
	}

	user.TOTPSecret = secret

	if _, err := s.users.Update(ctx, user); err != nil {
		return "", "", err
	}

	return secret, security.TOTPURI(issuer, user.Email, secret), nil
}

// ConfirmTOTP activates two-factor authentication once the user proves they
// can generate a valid code.
func (s *SessionService) ConfirmTOTP(ctx context.Context, userID uint, code string) error {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return err
	}

	if user.TOTPSecret == "" {
		return apperror.Conflictf("start the two-factor setup first")
	}

	if !security.VerifyTOTP(user.TOTPSecret, code) {
		return apperror.Invalidf("the two-factor code is not valid")
	}

	user.TOTPEnabled = true

	_, err = s.users.Update(ctx, user)

	return err
}

// DisableTOTP turns two-factor authentication off again. The current code is
// required, so someone at an unlocked screen cannot silently remove it.
func (s *SessionService) DisableTOTP(ctx context.Context, userID uint, code string) error {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return err
	}

	if !user.TOTPEnabled {
		return apperror.Conflictf("two-factor authentication is not enabled")
	}

	if !security.VerifyTOTP(user.TOTPSecret, code) {
		return apperror.Invalidf("the two-factor code is not valid")
	}

	user.TOTPEnabled = false
	user.TOTPSecret = ""

	_, err = s.users.Update(ctx, user)

	return err
}

// SetLanguage stores the user's interface language.
func (s *SessionService) SetLanguage(ctx context.Context, userID uint, language string) error {
	if !model.IsSupportedLanguage(language) {
		return apperror.InvalidFields("language")
	}

	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return err
	}

	user.Language = language

	_, err = s.users.Update(ctx, user)

	return err
}
