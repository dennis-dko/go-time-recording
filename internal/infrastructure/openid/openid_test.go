package openid_test

import (
	"context"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/openid"
	"github.com/dennis-dko/go-time-recording/test/oidctest"
)

func configFor(p *oidctest.Provider) model.OpenIDConfig {
	return model.OpenIDConfig{
		Enabled: true, Issuer: p.URL, ClientID: oidctest.ClientID, ClientSecret: oidctest.ClientSecret,
	}
}

func aRound() model.OpenIDRound {
	return model.OpenIDRound{
		State:       "the-state-of-this-round",
		Nonce:       "the-nonce-of-this-round",
		Verifier:    "a-verifier-long-enough-for-pkce-0123456789abcdef",
		RedirectURI: "http://time.example/oidc-callback",
	}
}

// sendOff asks for the address to send a browser to, and follows it to where the
// provider sends the browser back.
func sendOff(t *testing.T, c *openid.Client, config model.OpenIDConfig, round model.OpenIDRound) (code string) {
	t.Helper()

	address, err := c.AuthCodeURL(context.Background(), config, round)
	if err != nil {
		t.Fatalf("the address to send the browser to: %v", err)
	}

	back := oidctest.Follow(t, address)

	if got := back.Scheme + "://" + back.Host + back.Path; got != round.RedirectURI {
		t.Fatalf("the provider sent the browser back to %s, want %s", got, round.RedirectURI)
	}

	if state := back.Query().Get("state"); state != round.State {
		t.Fatalf("the provider sent back the state %q, want %q", state, round.State)
	}

	return back.Query().Get("code")
}

// A sign-in goes to the provider and comes back with who the provider says it is:
// its issuer, the subject it gives that person, and their address and name.
func TestASignInGoesThroughTheProviderAndBack(t *testing.T) {
	p := oidctest.Start(t)
	c := openid.New(5 * time.Second)
	round := aRound()

	p.SignInAs(oidctest.Person{Subject: "ida-subject", Email: "ida@example.com", Name: "Ida"})

	code := sendOff(t, c, configFor(p), round)

	claims, err := c.Identify(context.Background(), configFor(p), code, round)
	if err != nil {
		t.Fatalf("identify: %v", err)
	}

	want := model.OpenIDClaims{Issuer: p.URL, Subject: "ida-subject", Email: "ida@example.com", Name: "Ida"}
	if *claims != want {
		t.Errorf("the provider says %+v came back, want %+v", *claims, want)
	}
}

// A token is only believed if it was signed with the provider's published key, for
// this installation, by this provider, for this round, and has not expired.
func TestATokenNotMadeForThisRoundIsRefused(t *testing.T) {
	for name, spoil := range map[string]func(*oidctest.Provider){
		"another round's nonce": func(p *oidctest.Provider) {
			p.TamperWithNextToken(func(claims map[string]any) { claims["nonce"] = "another" })
		},
		"another client's token": func(p *oidctest.Provider) {
			p.TamperWithNextToken(func(claims map[string]any) { claims["aud"] = "another-client" })
		},
		"another issuer's token": func(p *oidctest.Provider) {
			p.TamperWithNextToken(func(claims map[string]any) { claims["iss"] = "http://elsewhere.example" })
		},
		"an expired token": func(p *oidctest.Provider) {
			p.TamperWithNextToken(func(claims map[string]any) {
				claims["exp"] = time.Now().Add(-time.Hour).Unix()
			})
		},
		"a token signed with an unpublished key": func(p *oidctest.Provider) { p.ForgeNextToken() },
	} {
		t.Run(name, func(t *testing.T) {
			p := oidctest.Start(t)
			c := openid.New(5 * time.Second)
			round := aRound()

			code := sendOff(t, c, configFor(p), round)
			spoil(p)

			if claims, err := c.Identify(context.Background(), configFor(p), code, round); err == nil {
				t.Errorf("believed %+v", *claims)
			}
		})
	}
}

// A code is only traded with the verifier its challenge was made from, and only by
// this installation's own client: one intercepted on its way back is worth nothing
// to anybody else.
func TestACodeIsTradedOnlyByWhoAskedForIt(t *testing.T) {
	p := oidctest.Start(t)
	c := openid.New(5 * time.Second)
	round := aRound()

	code := sendOff(t, c, configFor(p), round)

	stolen := round
	stolen.Verifier = "somebody-elses-verifier-long-enough-for-pkce-0123"

	if _, err := c.Identify(context.Background(), configFor(p), code, stolen); err == nil {
		t.Error("a code was traded without the verifier it was asked for with")
	}

	code = sendOff(t, c, configFor(p), round)

	impostor := configFor(p)
	impostor.ClientSecret = "not-the-secret"

	if _, err := c.Identify(context.Background(), impostor, code, round); err == nil {
		t.Error("a code was traded with the wrong client secret")
	}

	if p.Exchanged() != 0 {
		t.Errorf("the provider traded %d code(s), want none", p.Exchanged())
	}
}

// A provider that cannot be reached says so, rather than hanging the sign-in.
func TestAProviderThatCannotBeReachedSaysSo(t *testing.T) {
	c := openid.New(2 * time.Second)
	config := model.OpenIDConfig{Enabled: true, Issuer: "http://127.0.0.1:1", ClientID: "x"}

	if _, err := c.AuthCodeURL(context.Background(), config, aRound()); err == nil {
		t.Error("an address was made for a provider nobody answers for")
	}

	if err := c.Check(context.Background(), config.Issuer); err == nil {
		t.Error("a provider nobody answers for passed the check")
	}
}
