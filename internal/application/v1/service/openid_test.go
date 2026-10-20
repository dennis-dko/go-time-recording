package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/memory"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// fakeProvider answers as a provider does, for the application's half of a round:
// the code it hands back is the round's own, and it says whoever it is told to.
type fakeProvider struct {
	who     model.OpenIDClaims
	refuse  error
	traded  int
	address string
}

func (p *fakeProvider) AuthCodeURL(_ context.Context, config model.OpenIDConfig, round model.OpenIDRound) (string, error) {
	p.address = config.Issuer + "/authorize?state=" + round.State + "&redirect=" + round.RedirectURI

	return p.address, nil
}

func (p *fakeProvider) Identify(
	_ context.Context,
	config model.OpenIDConfig,
	code string,
	round model.OpenIDRound,
) (*model.OpenIDClaims, error) {
	p.traded++

	if p.refuse != nil {
		return nil, p.refuse
	}

	if code != codeFor(round.State) {
		return nil, errors.New("the provider handed out no such code for this round")
	}

	who := p.who
	who.Issuer = config.Issuer

	return &who, nil
}

func (p *fakeProvider) Check(context.Context, string) error { return nil }

// codeFor is the code the fake provider sends a round back with.
func codeFor(state string) string { return "code-for-" + state }

// openIDFixture is the booking fixture with a provider to sign in through.
type openIDFixture struct {
	*fixture

	provider *fakeProvider
	config   model.OpenIDConfig
	openid   *service.OpenIDService
}

func newOpenIDFixture(t *testing.T) *openIDFixture {
	t.Helper()

	f := &openIDFixture{
		fixture:  newFixture(t),
		provider: &fakeProvider{who: model.OpenIDClaims{Subject: "dennis-at-the-provider", Email: "dennis@example.com"}},
		config:   model.OpenIDConfig{Enabled: true, Issuer: "https://id.example.com", ClientID: "gtr"},
	}

	source := service.OpenIDConfigSource(func(context.Context) (model.OpenIDConfig, error) { return f.config, nil })
	f.openid = service.NewOpenIDService(memory.NewOpenIDIdentityRepository(), f.userRepo, source, f.provider)

	return f
}

const callback = "https://time.example.com/sign-in/openid"

// link links an account to whoever the provider says, by the whole round.
func (f *openIDFixture) link(t *testing.T, userID uint) error {
	t.Helper()

	_, state, err := f.openid.BeginLink(context.Background(), userID, callback)
	if err != nil {
		return err
	}

	outcome, err := f.openid.Finish(context.Background(), state, codeFor(state), userID)
	if err != nil {
		return err
	}

	if !outcome.Linked || outcome.User.ID != userID {
		t.Fatalf("linking %d came back as %+v", userID, outcome)
	}

	return nil
}

// signIn signs in through the provider, by the whole round, from no session.
func (f *openIDFixture) signIn(t *testing.T) (*service.OpenIDOutcome, error) {
	t.Helper()

	_, state, err := f.openid.BeginSignIn(context.Background(), callback)
	if err != nil {
		t.Fatalf("begin a sign-in: %v", err)
	}

	return f.openid.Finish(context.Background(), state, codeFor(state), 0)
}

// refusedWith asserts an error carries a code.
func refusedWith(t *testing.T, err error, code string) {
	t.Helper()

	detail, ok := apperror.Detail(err)
	if !ok || detail.Code != code {
		t.Errorf("answered %v, want the refusal %q", err, code)
	}
}

// An identity its account's owner linked signs that account in, and the round
// sends the browser to the provider with the address it is to come back to.
func TestALinkedIdentitySignsItsAccountIn(t *testing.T) {
	f := newOpenIDFixture(t)

	if err := f.link(t, f.userID); err != nil {
		t.Fatalf("link: %v", err)
	}

	outcome, err := f.signIn(t)
	if err != nil {
		t.Fatalf("sign in: %v", err)
	}

	if outcome.Linked || outcome.User.ID != f.userID {
		t.Errorf("the sign-in came back as %+v, want the linked account signed in", outcome)
	}

	if want := "https://id.example.com/authorize?state="; len(f.provider.address) <= len(want) ||
		f.provider.address[:len(want)] != want {
		t.Errorf("the browser is sent to %q, want the provider's own sign-in", f.provider.address)
	}

	linked, err := f.openid.Linked(context.Background(), f.userID)
	if err != nil || linked == nil || linked.Subject != "dennis-at-the-provider" || linked.Issuer != f.config.Issuer {
		t.Errorf("the account reads as linked to %+v, %v", linked, err)
	}
}

// An identity nobody linked opens nothing: no account is matched on its address,
// and none is made for it.
func TestAnIdentityNobodyLinkedOpensNothing(t *testing.T) {
	f := newOpenIDFixture(t)

	// The provider says the fixture's address, which an account here has.
	f.provider.who.Email = "dennis@example.com"

	_, err := f.signIn(t)
	refusedWith(t, err, "openidNotLinked")
}

// A round is finished once, by the session that began it, and with the provider
// that was configured when it began.
func TestARoundIsFinishedOnceByWhoBeganIt(t *testing.T) {
	f := newOpenIDFixture(t)
	ctx := context.Background()

	_, state, err := f.openid.BeginSignIn(ctx, callback)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	if _, err := f.openid.Finish(ctx, state, codeFor(state), 0); err == nil {
		t.Fatal("an identity nobody linked was signed in")
	}

	_, err = f.openid.Finish(ctx, state, codeFor(state), 0)
	refusedWith(t, err, "attemptExpired")

	colleague, err := f.users.CreateUser(ctx, command.CreateUserCommand{
		Name: "Colleague", Email: "colleague@example.com", Role: model.RoleUser,
	})
	if err != nil {
		t.Fatalf("create a colleague: %v", err)
	}

	_, state, err = f.openid.BeginLink(ctx, f.userID, callback)
	if err != nil {
		t.Fatalf("begin a link: %v", err)
	}

	_, err = f.openid.Finish(ctx, state, codeFor(state), colleague.Result.ID)
	refusedWith(t, err, "openidRefused")

	if linked, _ := f.openid.Linked(ctx, f.userID); linked != nil {
		t.Errorf("a link begun by one session and finished by another was made: %+v", linked)
	}

	_, state, err = f.openid.BeginSignIn(ctx, callback)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	traded := f.provider.traded
	f.config.Issuer = "https://another-provider.example.com"

	_, err = f.openid.Finish(ctx, state, codeFor(state), 0)
	refusedWith(t, err, "attemptExpired")

	if f.provider.traded != traded {
		t.Error("a round begun with another provider had its code traded")
	}
}

// The built-in administrator and an account the directory holds are never linked,
// and an account that came to be held by the directory after it was linked is not
// signed in by its link: the directory's decision that somebody may still sign in
// is asked by its password and by nothing else.
func TestNeitherTheBuiltInNorADirectoryAccountIsSignedInThroughAProvider(t *testing.T) {
	f := newOpenIDFixture(t)
	ctx := context.Background()

	if _, err := f.auth.EnsureSystemUser(ctx); err != nil {
		t.Fatalf("make the built-in administrator: %v", err)
	}

	admin, err := f.userRepo.GetByEmail(ctx, service.SystemUserEmail)
	if err != nil {
		t.Fatalf("read the built-in administrator: %v", err)
	}

	_, _, err = f.openid.BeginLink(ctx, admin.ID, callback)
	refusedWith(t, err, "adminHasNoOpenID")

	if err := f.link(t, f.userID); err != nil {
		t.Fatalf("link: %v", err)
	}

	user, err := f.userRepo.GetByID(ctx, f.userID)
	if err != nil {
		t.Fatalf("read the account: %v", err)
	}

	user.IsExternal = true
	if _, err := f.userRepo.Update(ctx, user); err != nil {
		t.Fatalf("hand the account to the directory: %v", err)
	}

	_, err = f.signIn(t)
	refusedWith(t, err, "directoryAccountHasNoOpenID")

	_, _, err = f.openid.BeginLink(ctx, f.userID, callback)
	refusedWith(t, err, "directoryAccountHasNoOpenID")
}

// An identity linked to one account is not linked to a second: the second would
// otherwise sign in as whoever the first is, or the first would lose its link
// without being asked.
func TestAnIdentityLinkedToSomebodyIsNotLinkedAgain(t *testing.T) {
	f := newOpenIDFixture(t)
	ctx := context.Background()

	if err := f.link(t, f.userID); err != nil {
		t.Fatalf("link: %v", err)
	}

	colleague, err := f.users.CreateUser(ctx, command.CreateUserCommand{
		Name: "Colleague", Email: "colleague@example.com", Role: model.RoleUser,
	})
	if err != nil {
		t.Fatalf("create a colleague: %v", err)
	}

	refusedWith(t, f.link(t, colleague.Result.ID), "openidLinkedElsewhere")

	if outcome, err := f.signIn(t); err != nil || outcome.User.ID != f.userID {
		t.Errorf("after the refusal the identity signs in %+v, %v - want the first account", outcome, err)
	}
}

// Switched off, the provider is neither offered nor gone to, and a provider that
// refuses the code signs nobody in.
func TestNothingGoesThroughAProviderThatIsOffOrRefuses(t *testing.T) {
	f := newOpenIDFixture(t)
	ctx := context.Background()

	if err := f.link(t, f.userID); err != nil {
		t.Fatalf("link: %v", err)
	}

	f.provider.refuse = errors.New("invalid_grant")

	_, err := f.signIn(t)
	refusedWith(t, err, "openidRefused")

	f.provider.refuse = nil
	f.config.Enabled = false

	if offered, _, err := f.openid.Offered(ctx); err != nil || offered {
		t.Errorf("a provider switched off is offered: %v, %v", offered, err)
	}

	_, _, err = f.openid.BeginSignIn(ctx, callback)
	refusedWith(t, err, "openidOff")
}
