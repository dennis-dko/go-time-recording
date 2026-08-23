package service

import (
	"context"
	"strconv"
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
var ErrTOTPRequired = apperror.Conflictf("a two-factor code is required").WithCode("twoFactorRequired")

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

	metrics
}

// WithMetrics attaches the recorder. Optional: without it sign-in works and
// records nothing.
func (s *SessionService) WithMetrics(recorder Recorder) *SessionService {
	s.recorder = recorder

	return s
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
		defaultRole: model.RoleUser,
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
		// Counted apart, because they mean different things to whoever is
		// looking: a rising "credentials" is somebody working through a password
		// list, and a rising "directory" is a directory that has stopped
		// answering - which turns away people whose passwords are perfectly good.
		reason := SignInFailureCredentials
		if apperror.KindOf(err) == apperror.KindInternal {
			reason = SignInFailureDirectory
		}

		s.count(ctx, MetricSignInFailures, "reason", reason)

		return nil, err
	}

	if user.TOTPEnabled {
		// Not a failure: the password was right and the client is being asked
		// for the second factor it was always going to be asked for.
		if totpCode == "" {
			return nil, ErrTOTPRequired
		}

		if !security.VerifyTOTP(user.TOTPSecret, totpCode) {
			s.count(ctx, MetricSignInFailures, "reason", SignInFailureTOTP)

			return nil, apperror.Invalidf("the two-factor code is not valid").WithCode("twoFactorCodeInvalid")
		}
	}

	return s.OpenSession(ctx, user)
}

// OpenSession issues a session for a user whose identity has already been
// established.
//
// Split out of Login because a password is not the only way to establish it: a
// passkey signature does the same, and the session that follows has to be
// identical in every respect - same lifetime, same storage, same principal.
// Two code paths building sessions would eventually build two different ones.
//
// It performs no authentication of its own, so every caller has to have done
// that first.
func (s *SessionService) OpenSession(ctx context.Context, user *model.User) (*LoginResult, error) {
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

		// A session nobody ever uses is as idle as one abandoned after a day's
		// work, so the clock starts here rather than at the first request.
		LastSeenAt: now,
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
	invalid := apperror.Invalidf("invalid credentials").WithCode("invalidCredentials")

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

	// What there is to check against, which is nothing at all in three of the
	// four cases: no such address, an account the directory owns and this holds
	// no usable password for, or an account with no password set.
	//
	// Nothing is still checked. VerifyPassword spends the same time on an empty
	// hash as on a real one, and that time is the entire cost of a sign-in -
	// bcrypt is tens of milliseconds and everything around it is one. Returning
	// before it, as this did, refused an address nobody holds in a millisecond
	// and a real one in sixty. Same status, same sentence, and a gap wide enough
	// to read a staff list out of with one request per guess.
	//
	// A directory-backed account was the same tell in a quieter form: it says
	// which of the addresses that do exist are managed elsewhere.
	stored := ""
	if localErr == nil && !local.IsExternal {
		stored = local.PasswordHash
	}

	matches := security.VerifyPassword(stored, password)

	// Judged after, in the order that keeps the dereference safe: IsExternal is
	// only read when there is a local account to read it from.
	if localErr != nil || local.IsExternal || !matches {
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
		return nil, apperror.Invalidf("invalid credentials").WithCode("invalidCredentials")
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
		return nil, apperror.Invalidf("invalid credentials").WithCode("invalidCredentials")
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
		return nil, apperror.Invalidf("no session").WithCode("noSession")
	}

	session, err := s.sessions.Get(ctx, security.HashToken(token))
	if err != nil {
		return nil, apperror.Invalidf("no session").WithCode("noSession")
	}

	now := time.Now()

	// Two different questions, and both end the session.
	//
	// The lifetime is absolute and starts at the sign-in: a bound on how long one
	// act of proving who you are is worth. Idleness is measured from the last use
	// and asks whether anybody is still there - a person working all morning keeps
	// their session, and the same person going home at noon loses it.
	idle := s.idleTimeout(ctx)

	if session.Expired(now) || session.Idle(now, idle) {
		// Clean up on the way past, so expired rows do not need the sweep to
		// disappear the moment someone tries to use them.
		_ = s.sessions.Delete(ctx, session.TokenHash)

		return nil, apperror.Invalidf("session expired").WithCode("sessionExpired")
	}

	s.touch(ctx, session, now, idle)

	user, err := s.users.GetByID(ctx, session.UserID)
	if err != nil {
		return nil, apperror.Invalidf("no session").WithCode("noSession")
	}

	return s.auth.principalFor(ctx, user)
}

// idleTimeout is how long a session may go unused, or zero for no limit.
func (s *SessionService) idleTimeout(ctx context.Context) time.Duration {
	if s.limits == nil {
		return 0
	}

	return s.limits.SessionIdle(ctx)
}

// maxTouchInterval is the coarsest the recorded last-use is ever allowed to get.
//
// Not every request. Resolve runs on all of them, and a write per request turns
// reading a screen into a stream of updates to one row - the contention alone
// would cost more than the feature.
const maxTouchInterval = time.Minute

// touchInterval is how stale the recorded last-use may get before it is written
// again.
//
// Half the timeout, capped at a minute. The cap alone was the first attempt and
// it was wrong: with a timeout shorter than the interval, a session in constant
// use is never written down at all, so it goes on looking untouched since the
// sign-in and is ended while somebody is working in it. Which is not a
// theoretical shortness - it is what the case for this uses, and what an
// installation trying the feature out would set first.
//
// Half rather than the whole, so a request arriving just before the deadline
// still leaves a written record on the near side of it.
func touchInterval(idle time.Duration) time.Duration {
	if idle <= 0 || idle/2 > maxTouchInterval {
		return maxTouchInterval
	}

	return idle / 2
}

// touch records that the session is in use, without writing on every request.
//
// Failures are ignored on purpose. This is bookkeeping for a timeout, not the
// request somebody made: refusing to serve a screen because a timestamp could
// not be updated would turn a slow database into an outage.
func (s *SessionService) touch(
	ctx context.Context, session *model.Session, now time.Time, idle time.Duration,
) {
	if now.Sub(session.LastSeenAt) < touchInterval(idle) {
		return
	}

	_ = s.sessions.Touch(ctx, session.TokenHash, now)
}

// Logout ends one session.
func (s *SessionService) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}

	return s.sessions.Delete(ctx, security.HashToken(token))
}

// LogoutOthers ends every session of a user but the one the token belongs to.
//
// For somebody changing their own password: the other devices lose access, which
// is the point, and the one they are holding does not - it just proved it knows
// the old password, and signing it out mid-wizard achieves nothing but a second
// sign-in.
func (s *SessionService) LogoutOthers(ctx context.Context, userID uint, token string) error {
	if token == "" {
		// No session to keep - a token client, say - so every one of them goes.
		return s.sessions.DeleteForUser(ctx, userID)
	}

	return s.sessions.DeleteForUserExcept(ctx, userID, security.HashToken(token))
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
		return "", "", apperror.Conflictf("two-factor authentication is already enabled").
			WithCode("twoFactorAlreadyOn")
	}

	secret, err = security.NewTOTPSecret()
	if err != nil {
		return "", "", apperror.Internal(err)
	}

	// The secret alone, with the flag left off: an enrolment that is pending, not
	// one that is in force. Only these two columns, so a preference somebody
	// changes while enrolling is not reverted by this.
	if err := s.users.SetTOTP(ctx, user.ID, secret, false); err != nil {
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
		return apperror.Conflictf("start the two-factor setup first").WithCode("twoFactorNotStarted")
	}

	if !security.VerifyTOTP(user.TOTPSecret, code) {
		return apperror.Invalidf("the two-factor code is not valid").WithCode("twoFactorCodeInvalid")
	}

	// The same secret, now in force.
	return s.users.SetTOTP(ctx, user.ID, user.TOTPSecret, true)
}

// DisableTOTP turns two-factor authentication off again. The current code is
// required, so someone at an unlocked screen cannot silently remove it.
func (s *SessionService) DisableTOTP(ctx context.Context, userID uint, code string) error {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return err
	}

	if !user.TOTPEnabled {
		return apperror.Conflictf("two-factor authentication is not enabled").WithCode("twoFactorNotOn")
	}

	if !security.VerifyTOTP(user.TOTPSecret, code) {
		return apperror.Invalidf("the two-factor code is not valid").WithCode("twoFactorCodeInvalid")
	}

	// Off, and the secret gone with it: a secret left behind would sign somebody
	// in again the moment the flag was flipped back.
	return s.users.SetTOTP(ctx, user.ID, "", false)
}

// SetLanguage stores the user's interface language.
func (s *SessionService) SetLanguage(ctx context.Context, userID uint, language string) error {
	if !model.IsSupportedLanguage(language) {
		return apperror.InvalidFields("language")
	}

	return s.users.SetPreference(ctx, userID, repository.PreferenceLanguage, language)
}

// SetTheme stores the appearance this person reads in, or clears it.
//
// An empty value is the normal case rather than an omission: it means "follow
// the time of day", which is what somebody who has never chosen gets and what
// they go back to by choosing automatic.
func (s *SessionService) SetTheme(ctx context.Context, userID uint, theme string) error {
	if !model.IsSupportedTheme(theme) {
		return apperror.InvalidFields("theme")
	}

	return s.users.SetPreference(ctx, userID, repository.PreferenceTheme, theme)
}

// SetTourSeen records whether this person has been shown the guided tour.
//
// Settable both ways: someone who wants to see it again should be able to ask,
// rather than being told they have already had their chance.
func (s *SessionService) SetTourSeen(ctx context.Context, userID uint, seen bool) error {
	// One column, not the whole row: this is written the moment somebody signs in,
	// while they are already doing something else. Writing the row back would take
	// whatever that something else changed with it.
	return s.users.SetPreference(ctx, userID, repository.PreferenceTourSeen,
		strconv.FormatBool(seen))
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

	err := s.users.SetPreference(ctx, userID, repository.PreferenceTimezone, timezone)

	return err
}
