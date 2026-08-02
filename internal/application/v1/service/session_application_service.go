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
	// ID is the directory-side identifier that never changes. It is what
	// accounts are matched on, so a renamed mailbox does not read as a
	// departure followed by an arrival.
	ID string

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

	// limits, when set, supplies the session lifetime an administrator has
	// configured, so a change applies without a restart. It falls back to
	// lifetime when absent, which is what the tests rely on.
	limits *LimitsProvider

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

// WithLimits attaches the administered limits, so the session lifetime follows
// the Settings screen rather than only the environment.
func (s *SessionService) WithLimits(limits *LimitsProvider) *SessionService {
	s.limits = limits

	return s
}

// sessionLifetime is the administered value, or the one configured at start-up.
func (s *SessionService) sessionLifetime(ctx context.Context) time.Duration {
	if s.limits == nil {
		return s.lifetime
	}

	return s.limits.SessionLifetime(ctx)
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
		ExpiresAt: now.Add(s.sessionLifetime(ctx)),
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

	local, localErr := s.users.GetByEmail(ctx, normalizeEmail(email))

	// The built-in administrator is authenticated locally and only locally. It
	// exists so an installation always has a way back in, which it would not be
	// if whoever controls the directory could take it over by creating an entry
	// with this address.
	systemAccount := localErr == nil && local.IsSystem

	if !systemAccount && s.external != nil && s.external.Enabled() {
		directoryUser, ok, err := s.external.Authenticate(ctx, email, password)
		if err != nil {
			return nil, apperror.Internal(err)
		}

		if ok {
			return s.provisionExternal(ctx, directoryUser)
		}
	}

	if localErr != nil {
		return nil, invalid
	}

	// An account backed by the directory has no usable local password, so it
	// must not fall through to a local check.
	if local.IsExternal {
		return nil, invalid
	}

	if local.PasswordHash == "" || !security.VerifyPassword(local.PasswordHash, password) {
		return nil, invalid
	}

	return local, nil
}

// provisionExternal returns the local account for a directory user, creating
// it on first sign-in.
//
// The stable identifier is tried first. Falling back to the mail address
// covers accounts created before identifiers were recorded, and adopting the
// identifier on the way through means each account is matched by address at
// most once.
func (s *SessionService) provisionExternal(ctx context.Context, directoryUser *ExternalUser) (*model.User, error) {
	email := normalizeEmail(directoryUser.Email)

	if directoryUser.ID != "" {
		existing, err := s.users.GetByExternalID(ctx, directoryUser.ID)
		if err == nil {
			return s.reconcileExternal(ctx, existing, directoryUser, email)
		}
	}

	existing, err := s.users.GetByEmail(ctx, email)
	if err == nil {
		return s.reconcileExternal(ctx, existing, directoryUser, email)
	}

	// A directory entry must never bring the built-in administrator into
	// existence, or the account meant as the way back in would be one the
	// directory controls.
	if email == SystemUserEmail {
		return nil, apperror.Invalidf("invalid credentials")
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
		ExternalID:       directoryUser.ID,
		DailyTargetHours: model.DefaultDailyTargetHours,
	})
}

// reconcileExternal keeps the local copy in step with the directory: it adopts
// the stable identifier if it is not stored yet, and follows a renamed mailbox
// instead of treating it as a different person.
func (s *SessionService) reconcileExternal(
	ctx context.Context,
	existing *model.User,
	directoryUser *ExternalUser,
	email string,
) (*model.User, error) {
	// The built-in administrator stays local whatever the directory says. Not
	// reachable through the normal sign-in path, which never consults the
	// directory for it, but a stored identifier could still lead here.
	if existing.IsSystem {
		return nil, apperror.Invalidf("invalid credentials")
	}

	changed := false

	if directoryUser.ID != "" && existing.ExternalID != directoryUser.ID {
		existing.ExternalID = directoryUser.ID
		changed = true
	}

	if email != "" && existing.Email != email {
		existing.Email = email
		changed = true
	}

	if directoryUser.Name != "" && existing.Name != directoryUser.Name {
		existing.Name = directoryUser.Name
		changed = true
	}

	if !changed {
		return existing, nil
	}

	return s.users.Update(ctx, existing)
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

// SetTourSeen records whether this person has been shown the guided tour.
//
// Settable both ways: someone who wants to see it again should be able to ask,
// rather than being told they have already had their chance.
func (s *SessionService) SetTourSeen(ctx context.Context, userID uint, seen bool) error {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return err
	}

	user.TourSeen = seen

	_, err = s.users.Update(ctx, user)

	return err
}

// SetTimezone stores the user's own zone, or clears it so they follow the
// instance setting again.
//
// An empty name is the normal case and is deliberately allowed: most people
// should move with the instance rather than be pinned to whatever zone they
// happened to be in when the account was made.
func (s *SessionService) SetTimezone(ctx context.Context, userID uint, timezone string) error {
	timezone = strings.TrimSpace(timezone)

	if timezone != "" && !model.IsSupportedTimezone(timezone) {
		return apperror.InvalidFields("timezone")
	}

	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return err
	}

	user.Timezone = timezone

	_, err = s.users.Update(ctx, user)

	return err
}
