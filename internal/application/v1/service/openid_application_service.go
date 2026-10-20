package service

import (
	"context"
	"strconv"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
	"github.com/dennis-dko/go-time-recording/internal/support/security"
)

// openIDRoundLifetime is how long a round may spend at the provider: a password
// and a second factor typed there, by somebody who may have to find their phone
// first - longer than a passkey's challenge, which asks for one gesture.
const openIDRoundLifetime = 10 * time.Minute

// OpenIDProvider is a sign-in provider, as the application asks it things: where
// to send a browser, who the provider says came back, and whether it can be found
// at all.
type OpenIDProvider interface {
	AuthCodeURL(ctx context.Context, config model.OpenIDConfig, round model.OpenIDRound) (string, error)
	Identify(ctx context.Context, config model.OpenIDConfig, code string, round model.OpenIDRound) (*model.OpenIDClaims, error)
	Check(ctx context.Context, issuer string) error
}

// OpenIDConfigSource reads the provider people sign in through, per call, so that
// a change on the Settings screen holds from the next sign-in on.
type OpenIDConfigSource func(ctx context.Context) (model.OpenIDConfig, error)

// OpenIDService signs people in through a provider - Keycloak, Authentik, Entra
// ID, Google - and links each account to its identity there.
//
// Only by a link its owner made. An identity is never matched to an account on
// arrival, by its address or anything else, and never makes an account of its
// own: an address is the one claim a provider is least careful about, and an
// installation whose accounts are made by its administrator should not have them
// made by whoever can sign in at Google. So the owner links it while signed in -
// which they could only be with their password, and its second factor - and from
// then on the provider's sign-in is theirs to use instead, its own second factor
// included.
//
// The built-in administrator and an account the directory holds are never
// signed in this way, for the reasons a passkey is refused them.
type OpenIDService struct {
	identities repository.OpenIDIdentityRepository
	users      repository.UserRepository
	config     OpenIDConfigSource
	provider   OpenIDProvider

	// pending holds each round between the browser leaving for the provider and
	// coming back, keyed by its state; see pendingStore.
	pending pendingStore[openIDPending]
}

// openIDPending is a round on its way through the provider.
type openIDPending struct {
	round model.OpenIDRound

	// issuer is the provider the round began with. A provider changed while the
	// browser was away makes the round one nobody can finish, rather than one
	// whose code is traded with a provider that never issued it.
	issuer string

	// linkFor is the account a link is being made for, zero for a sign-in.
	linkFor uint
}

// NewOpenIDService makes the service with no round in flight.
func NewOpenIDService(
	identities repository.OpenIDIdentityRepository,
	users repository.UserRepository,
	config OpenIDConfigSource,
	provider OpenIDProvider,
) *OpenIDService {
	return &OpenIDService{
		identities: identities, users: users, config: config, provider: provider,
		pending: pendingStore[openIDPending]{lifetime: openIDRoundLifetime},
	}
}

// OpenIDOutcome is what a round came back with.
type OpenIDOutcome struct {
	// User is who signed in, or whose account was linked.
	User *model.User

	// Linked says the round linked an account rather than signing somebody in.
	Linked bool
}

// Offered says whether the sign-in form offers the provider, and what it calls it.
func (s *OpenIDService) Offered(ctx context.Context) (offered bool, label string, err error) {
	config, err := s.config(ctx)
	if err != nil {
		return false, "", err
	}

	return config.Offered(), config.Label, nil
}

// BeginSignIn starts a sign-in through the provider, and answers with the address
// to send the browser to and the round's state.
func (s *OpenIDService) BeginSignIn(ctx context.Context, redirectURI string) (address, state string, err error) {
	return s.begin(ctx, redirectURI, 0)
}

// BeginLink starts linking an account to its owner's identity at the provider.
func (s *OpenIDService) BeginLink(
	ctx context.Context,
	userID uint,
	redirectURI string,
) (address, state string, err error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return "", "", err
	}

	if err := mayGoThroughAProvider(user); err != nil {
		return "", "", err
	}

	return s.begin(ctx, redirectURI, userID)
}

func (s *OpenIDService) begin(ctx context.Context, redirectURI string, linkFor uint) (address, state string, err error) {
	config, err := s.config(ctx)
	if err != nil {
		return "", "", err
	}

	if !config.Offered() {
		return "", "", apperror.Conflictf("signing in through a provider is switched off").WithCode("openidOff")
	}

	round := model.OpenIDRound{RedirectURI: redirectURI}

	for _, secret := range []*string{&round.State, &round.Nonce, &round.Verifier} {
		if *secret, err = security.NewSessionToken(); err != nil {
			return "", "", apperror.Internal(err)
		}
	}

	address, err = s.provider.AuthCodeURL(ctx, config, round)
	if err != nil {
		return "", "", providerFailure(apperror.Conflictf("the sign-in provider could not be reached").
			WithCode("openidUnreachable"), err)
	}

	s.pending.put(round.State, openIDPending{round: round, issuer: config.Issuer, linkFor: linkFor}, time.Now())

	return address, round.State, nil
}

// Finish ends a round the browser came back from with a code: it links the
// account the round was begun for, or signs in the account the identity is linked
// to.
//
// signedIn is the account of the session the browser came back with, zero for
// none. A link is made only for the account whose session began it, so a round
// begun in somebody's session cannot be finished in another's.
func (s *OpenIDService) Finish(ctx context.Context, state, code string, signedIn uint) (*OpenIDOutcome, error) {
	expired := apperror.Invalidf("this attempt has expired, please try again").WithCode("attemptExpired")

	waiting, ok := s.pending.take(state, time.Now())
	if !ok {
		return nil, expired
	}

	config, err := s.config(ctx)
	if err != nil {
		return nil, err
	}

	if !config.Offered() || config.Issuer != waiting.issuer {
		return nil, expired
	}

	if waiting.linkFor != 0 && signedIn != waiting.linkFor {
		return nil, apperror.Conflictf("the sign-in provider did not confirm this").WithCode("openidRefused")
	}

	claims, err := s.provider.Identify(ctx, config, code, waiting.round)
	if err != nil {
		return nil, providerFailure(apperror.Conflictf("the sign-in provider did not confirm this").
			WithCode("openidRefused"), err)
	}

	if waiting.linkFor != 0 {
		return s.link(ctx, waiting.linkFor, claims)
	}

	identity, err := s.identities.BySubject(ctx, claims.Issuer, claims.Subject)
	if err != nil {
		return nil, missingOr(err, apperror.Conflictf("this identity is linked to no account here").
			WithCode("openidNotLinked"))
	}

	user, err := s.users.GetByID(ctx, identity.UserID)
	if err != nil {
		return nil, err
	}

	// Asked again here, because an account linked while it was local can since
	// have been adopted by the directory.
	if err := mayGoThroughAProvider(user); err != nil {
		return nil, err
	}

	return &OpenIDOutcome{User: user}, nil
}

// link links an account to the identity a round came back with.
func (s *OpenIDService) link(ctx context.Context, userID uint, claims *model.OpenIDClaims) (*OpenIDOutcome, error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	if err := mayGoThroughAProvider(user); err != nil {
		return nil, err
	}

	existing, err := s.identities.BySubject(ctx, claims.Issuer, claims.Subject)
	if err == nil && existing.UserID != userID {
		return nil, apperror.Conflictf("this identity is linked to another account").
			WithCode("openidLinkedElsewhere")
	}

	if err != nil && apperror.KindOf(err) != apperror.KindNotFound {
		return nil, err
	}

	if err := s.identities.Link(ctx, &model.OpenIDIdentity{
		UserID: userID, Issuer: claims.Issuer, Subject: claims.Subject, CreatedAt: time.Now(),
	}); err != nil {
		return nil, err
	}

	return &OpenIDOutcome{User: user, Linked: true}, nil
}

// Linked returns the identity an account is linked to, or nil when it has none.
func (s *OpenIDService) Linked(ctx context.Context, userID uint) (*model.OpenIDIdentity, error) {
	identity, err := s.identities.ForUser(ctx, userID)
	if apperror.KindOf(err) == apperror.KindNotFound {
		return nil, nil
	}

	return identity, err
}

// Unlink takes an account's link away, after which the provider signs it in no
// more.
func (s *OpenIDService) Unlink(ctx context.Context, userID uint) error {
	removed, err := s.identities.Unlink(ctx, userID)
	if err != nil {
		return err
	}

	if !removed {
		return apperror.NotFound("identity", strconv.FormatUint(uint64(userID), 10))
	}

	return nil
}

// Check asks whether a provider can be found, which the Settings screen does
// before anybody's sign-in depends on it.
func (s *OpenIDService) Check(ctx context.Context, issuer string) error {
	if err := s.provider.Check(ctx, issuer); err != nil {
		return providerFailure(apperror.Conflictf("the sign-in provider could not be reached").
			WithCode("openidUnreachable"), err)
	}

	return nil
}

// mayGoThroughAProvider refuses the two accounts a provider never signs in.
//
// The built-in administrator, because it is the guaranteed way back into an
// installation, and a way back in that depends on somebody else's service being up
// is not one. And an account the directory holds, because its password is the
// directory's, and so is the decision that it may still sign in: removing or
// locking the entry there is how an organisation ends somebody's access, and a
// provider asks the directory nothing.
func mayGoThroughAProvider(user *model.User) error {
	if user.IsSystem {
		return apperror.Conflictf("the built-in administrator signs in with its password, so that an " +
			"installation is never locked out by a provider it depends on").
			WithCode("adminHasNoOpenID")
	}

	if user.IsExternal {
		return apperror.Conflictf("an account from the directory signs in with the directory's password, " +
			"so that the directory can end it").
			WithCode("directoryAccountHasNoOpenID")
	}

	return nil
}

// providerFailure is a round the provider could not complete, with the provider's
// own reason kept underneath for the log: what reaches the person is the code,
// and what the provider said is the operator's to read.
//
// It takes the refusal whole rather than its code, so that every code is written
// where it is raised - which is where the checks that every code the server sends
// has a sentence go looking for them.
func providerFailure(failure *apperror.Error, cause error) error {
	failure.Err = cause

	return failure
}
