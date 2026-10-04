package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// challengeLifetime is how long a registration or sign-in may take.
//
// Long enough to find a phone and hold a thumb to it, short enough that a
// challenge overheard somewhere is useless by the time it is replayed.
const challengeLifetime = 5 * time.Minute

// PasskeyService registers WebAuthn credentials and verifies sign-ins with
// them.
//
// Passkeys are an additional way in, never a replacement: a password still
// works, and the built-in administrator is deliberately excluded from
// registering one at all. It is the guaranteed way back into an installation,
// and a guarantee that depends on a particular phone still existing is not one.
type PasskeyService struct {
	passkeys repository.PasskeyRepository
	users    repository.UserRepository

	// pending holds challenges between the two halves of a ceremony. In memory
	// on purpose: a challenge is worthless after five minutes, and putting it
	// in the database would mean writing a row for every sign-in attempt
	// including the abandoned ones.
	//
	// The cost is that a restart, or a second instance behind a load balancer,
	// loses them - the user retries and it works. Passwords are unaffected.
	pending challenges
}

// NewPasskeyService starts with no ceremony in flight. Challenges live in this
// process's memory, so a restart abandons any that were half done; see pending.
func NewPasskeyService(
	passkeys repository.PasskeyRepository,
	users repository.UserRepository,
) *PasskeyService {
	return &PasskeyService{passkeys: passkeys, users: users}
}

// RelyingParty is what the browser binds a credential to.
//
// It is derived from the request rather than configured, so an installation
// needs no setup for this to work: on localhost during development, and on
// whatever host serves it in production. The catch is that a credential is
// bound to that name for good - moving the application to a different domain
// makes existing passkeys unusable there, and everyone re-registers.
type RelyingParty struct {
	// ID is the domain, without port or scheme.
	ID string

	// Origin is the full scheme://host:port the browser will report.
	Origin string

	// DisplayName is what the prompt calls this installation.
	DisplayName string
}

// pendingSession is a challenge waiting for its second half.
type pendingSession struct {
	data    webauthn.SessionData
	expires time.Time
}

// Available reports whether passkeys can be used at all on this connection.
//
// Two conditions, and both are easy to miss:
//
//   - A secure context. Browsers expose WebAuthn over HTTPS, and over plain
//     HTTP only on localhost. On a plain-HTTP deployment the API would work
//     and every browser would refuse to call it.
//   - A domain name. An IP address is not a valid relying party identifier,
//     however the page was served: reaching the same instance on
//     http://127.0.0.1 rather than http://localhost fails with
//     "SecurityError: This is an invalid domain", which says nothing about
//     what to do instead.
//
// So the interface asks before offering the button. Offering one that can only
// produce that error would be worse than not offering it.
func (s *PasskeyService) Available(rp RelyingParty) bool {
	if net.ParseIP(rp.ID) != nil {
		return false
	}

	return strings.HasPrefix(rp.Origin, "https://") || rp.ID == "localhost"
}

// webAuthnFor builds a verifier bound to this request's origin.
//
// Per request rather than once at start-up, because the origin is only known
// then - and because an installation reachable under more than one name should
// work under each of them.
func (s *PasskeyService) webAuthnFor(rp RelyingParty) (*webauthn.WebAuthn, error) {
	name := rp.DisplayName
	if name == "" {
		name = model.FallbackAppTitle
	}

	w, err := webauthn.New(&webauthn.Config{
		RPID:          rp.ID,
		RPDisplayName: name,
		RPOrigins:     []string{rp.Origin},
	})
	if err != nil {
		return nil, apperror.Internal(err)
	}

	return w, nil
}

// ---------------------------------------------------------------- registering

// BeginRegistration produces the challenge the browser needs to create a
// credential.
func (s *PasskeyService) BeginRegistration(
	ctx context.Context,
	userID uint,
	rp RelyingParty,
) (options any, token string, err error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, "", err
	}

	// The built-in administrator stays on its password. It exists so an
	// installation always has a way back in, and a way back in that needs a
	// particular device is not one.
	if user.IsSystem {
		return nil, "", apperror.Conflictf(
			"the built-in administrator signs in with its password, so that an " +
				"installation is never locked out by a lost device").
			WithCode("adminHasNoPasskey")
	}

	// Nor an account the directory holds. Its password is the directory's, and
	// so is the decision that it may still sign in: a password sign-in asks the
	// directory every time, and removing or locking the entry there is how an
	// organisation ends somebody's access. A passkey asks nobody, so it would
	// keep opening sessions for a person the directory has let go - until a
	// synchronisation deletes the account, and by default none runs.
	if user.IsExternal {
		return nil, "", apperror.Conflictf(
			"an account from the directory signs in with the directory's password, " +
				"so that the directory can end it").
			WithCode("directoryAccountHasNoPasskey")
	}

	w, err := s.webAuthnFor(rp)
	if err != nil {
		return nil, "", err
	}

	existing, err := s.passkeys.GetByUser(ctx, userID)
	if err != nil {
		return nil, "", err
	}

	// Excluding what is already registered makes the authenticator say "you
	// already have one of these" instead of silently creating a duplicate.
	credentials := make([]protocol.CredentialDescriptor, 0, len(existing))
	for _, passkey := range existing {
		credentials = append(credentials, protocol.CredentialDescriptor{
			Type:         protocol.PublicKeyCredentialType,
			CredentialID: passkey.CredentialID,
		})
	}

	creation, session, err := w.BeginRegistration(
		&webAuthnUser{user: user},
		webauthn.WithExclusions(credentials),
		// Resident keys let someone sign in without typing a username: the
		// credential itself says who it belongs to.
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementPreferred),
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			// Required, so the device asks for a fingerprint or a PIN. Without
			// it, possession of an unlocked laptop would be the whole factor.
			UserVerification:   protocol.VerificationRequired,
			ResidentKey:        protocol.ResidentKeyRequirementPreferred,
			RequireResidentKey: protocol.ResidentKeyNotRequired(),
		}),
	)
	if err != nil {
		return nil, "", apperror.Internal(err)
	}

	token, err = s.storePending(*session)
	if err != nil {
		return nil, "", err
	}

	return creation, token, nil
}

// FinishRegistration verifies what the browser created and stores it.
func (s *PasskeyService) FinishRegistration(
	ctx context.Context,
	userID uint,
	rp RelyingParty,
	token, name string,
	response *protocol.ParsedCredentialCreationData,
) (*model.Passkey, error) {
	session, err := s.takePending(token)
	if err != nil {
		return nil, err
	}

	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	// The challenge was issued to this user; anyone else finishing it would be
	// registering a credential against somebody else's account.
	if string(session.UserID) != string(webAuthnID(user.ID)) {
		return nil, apperror.Invalidf("this registration belongs to a different sign-in").
			WithCode("passkeyWrongSession")
	}

	w, err := s.webAuthnFor(rp)
	if err != nil {
		return nil, err
	}

	credential, err := w.CreateCredential(&webAuthnUser{user: user}, session, response)
	if err != nil {
		return nil, apperror.Invalidf("the passkey could not be verified: %v", err).
			WithCode("passkeyUnverified")
	}

	label := strings.TrimSpace(name)
	if label == "" {
		label = model.DefaultPasskeyName
	}

	if len(label) > model.PasskeyMaxNameLength {
		label = label[:model.PasskeyMaxNameLength]
	}

	return s.passkeys.Save(ctx, &model.Passkey{
		UserID:          userID,
		Name:            label,
		CredentialID:    credential.ID,
		PublicKey:       credential.PublicKey,
		AttestationType: credential.AttestationType,
		Transports:      transportsToString(credential.Transport),
		SignCount:       credential.Authenticator.SignCount,
		BackupEligible:  credential.Flags.BackupEligible,
		BackedUp:        credential.Flags.BackupState,
		CreatedAt:       time.Now(),
	})
}

// -------------------------------------------------------------- signing in

// BeginLogin produces the challenge for a passwordless sign-in.
//
// No username is asked for: the browser offers whichever credentials it holds
// for this site, and the one that signs names its own owner. That is what makes
// a passkey sign-in a single gesture.
func (s *PasskeyService) BeginLogin(rp RelyingParty) (options any, token string, err error) {
	w, err := s.webAuthnFor(rp)
	if err != nil {
		return nil, "", err
	}

	assertion, session, err := w.BeginDiscoverableLogin(
		webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return nil, "", apperror.Internal(err)
	}

	token, err = s.storePending(*session)
	if err != nil {
		return nil, "", err
	}

	return assertion, token, nil
}

// FinishLogin verifies the signature and returns whose account it was.
func (s *PasskeyService) FinishLogin(
	ctx context.Context,
	rp RelyingParty,
	token string,
	response *protocol.ParsedCredentialAssertionData,
) (*model.User, error) {
	session, err := s.takePending(token)
	if err != nil {
		return nil, err
	}

	w, err := s.webAuthnFor(rp)
	if err != nil {
		return nil, err
	}

	invalid := apperror.Invalidf("the passkey was not accepted").WithCode("passkeyRejected")

	var matched *model.Passkey

	credential, err := w.ValidateDiscoverableLogin(
		func(rawID, _ []byte) (webauthn.User, error) {
			passkey, lookupErr := s.passkeys.GetByCredentialID(ctx, rawID)
			if lookupErr != nil {
				return nil, lookupErr
			}

			user, lookupErr := s.users.GetByID(ctx, passkey.UserID)
			if lookupErr != nil {
				return nil, lookupErr
			}

			matched = passkey

			return &webAuthnUser{user: user, credentials: []*model.Passkey{passkey}}, nil
		}, session, response)
	if err != nil || matched == nil {
		return nil, invalid
	}

	user, err := s.users.GetByID(ctx, matched.UserID)
	if err != nil {
		return nil, invalid
	}

	// A credential that outlived the reason it was trusted. Neither the built-in
	// administrator nor a directory account can register one, but an account
	// could still be holding one from before it became either - a local account
	// the directory later adopted is the ordinary case, and its passkey would
	// otherwise go on skipping the directory.
	if user.IsSystem || user.IsExternal {
		return nil, invalid
	}

	// Not fatal: the sign-in has already been verified, and refusing it because
	// a timestamp could not be written would be the wrong trade.
	_ = s.passkeys.TouchUsage(ctx, matched.ID, credential.Authenticator.SignCount)

	return user, nil
}

// ------------------------------------------------------------------ listing

// List returns a user's registered credentials.
func (s *PasskeyService) List(ctx context.Context, userID uint) ([]*model.Passkey, error) {
	return s.passkeys.GetByUser(ctx, userID)
}

// Delete revokes one credential.
//
// Deliberately allowed even when it is the last one: a passkey is an addition
// to the password, not a replacement, so removing it never locks anyone out.
func (s *PasskeyService) Delete(ctx context.Context, id, userID uint) error {
	return s.passkeys.Delete(ctx, id, userID)
}

// ------------------------------------------------------------------ pending

// challengeSweepInterval is how often the abandoned challenges are looked for,
// which is most of them: every sign-in somebody thinks better of leaves one.
//
// Not on every start, which is how it began: every start then visited every
// challenge still waiting, so the cost of a start grew with the number started -
// measured, 158 microseconds a start with 10,000 waiting and 11.6 milliseconds
// with 60,000, and the start of a sign-in needs no session. At most once a
// minute, an abandoned challenge outlives its lifetime by a minute at most and
// the visit is paid once rather than by every start.
const challengeSweepInterval = time.Minute

// challenges is the store PasskeyService.pending describes: a map behind a lock,
// swept of what has expired at most once a challengeSweepInterval.
//
// A plain map rather than a sync.Map, because when the last sweep was has to be
// kept with the map it describes, which is the case the sync package's own
// documentation sends to a map and a lock.
type challenges struct {
	mu        sync.Mutex
	byToken   map[string]pendingSession
	lastSweep time.Time
}

// put keeps a challenge until its second half takes it or it expires.
func (c *challenges) put(token string, data webauthn.SessionData, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if now.Sub(c.lastSweep) >= challengeSweepInterval {
		c.lastSweep = now

		for key, session := range c.byToken {
			if now.After(session.expires) {
				delete(c.byToken, key)
			}
		}
	}

	if c.byToken == nil {
		c.byToken = map[string]pendingSession{}
	}

	c.byToken[token] = pendingSession{data: data, expires: now.Add(challengeLifetime)}
}

// take returns a challenge and removes it, so one can never be used twice. One
// past its lifetime is refused whether or not a sweep has reached it yet.
func (c *challenges) take(token string, now time.Time) (webauthn.SessionData, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	session, ok := c.byToken[token]
	if !ok {
		return webauthn.SessionData{}, false
	}

	delete(c.byToken, token)

	if now.After(session.expires) {
		return webauthn.SessionData{}, false
	}

	return session.data, true
}

// storePending keeps a challenge and returns the handle for its second half.
//
// A random handle rather than the session cookie, because a sign-in has no
// session yet - that is the whole point of it.
func (s *PasskeyService) storePending(data webauthn.SessionData) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", apperror.Internal(err)
	}

	token := base64.RawURLEncoding.EncodeToString(buf)

	s.pending.put(token, data, time.Now())

	return token, nil
}

// takePending returns the challenge a handle stands for, once.
func (s *PasskeyService) takePending(token string) (webauthn.SessionData, error) {
	data, ok := s.pending.take(token, time.Now())
	if !ok {
		return webauthn.SessionData{}, apperror.Invalidf("this attempt has expired, please try again").
			WithCode("attemptExpired")
	}

	return data, nil
}

func transportsToString(transports []protocol.AuthenticatorTransport) string {
	parts := make([]string, 0, len(transports))
	for _, transport := range transports {
		parts = append(parts, string(transport))
	}

	return strings.Join(parts, ",")
}
