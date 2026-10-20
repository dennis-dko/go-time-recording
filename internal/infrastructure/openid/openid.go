package openid

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// errNoIdentityToken is a token answer without the identity token the scope
// "openid" asks for, which is a provider not speaking OpenID Connect at all.
var errNoIdentityToken = errors.New("the provider answered without an identity token")

// errAnotherRound is an identity token carrying another round's nonce: minted for
// some other sign-in and presented in this one.
var errAnotherRound = errors.New("the identity token was made for another sign-in")

// Client signs people in through OpenID Connect providers.
type Client struct {
	http *http.Client

	mu         sync.Mutex
	discovered map[string]*oidc.Provider
}

// New makes a client whose every request to a provider gives up after timeout,
// so a provider that has stopped answering costs a sign-in that long and no longer.
func New(timeout time.Duration) *Client {
	return &Client{http: &http.Client{Timeout: timeout}, discovered: map[string]*oidc.Provider{}}
}

// AuthCodeURL is the address to send a browser to for one round: the provider's
// own sign-in, asked to send the browser back to the round's address with a code
// that only the round's verifier can trade.
func (c *Client) AuthCodeURL(ctx context.Context, config model.OpenIDConfig, round model.OpenIDRound) (string, error) {
	provider, err := c.provider(ctx, config.Issuer)
	if err != nil {
		return "", err
	}

	return oauthConfig(provider, config, round).AuthCodeURL(round.State,
		oidc.Nonce(round.Nonce), oauth2.S256ChallengeOption(round.Verifier)), nil
}

// Identify trades the code the browser came back with for who the provider says
// it is - believed only from an identity token signed with a key the provider
// published, issued by it, for this installation's client, not expired, and
// carrying this round's nonce.
func (c *Client) Identify(
	ctx context.Context,
	config model.OpenIDConfig,
	code string,
	round model.OpenIDRound,
) (*model.OpenIDClaims, error) {
	provider, err := c.provider(ctx, config.Issuer)
	if err != nil {
		return nil, err
	}

	ctx = oidc.ClientContext(ctx, c.http)

	token, err := oauthConfig(provider, config, round).Exchange(ctx, code, oauth2.VerifierOption(round.Verifier))
	if err != nil {
		return nil, fmt.Errorf("trading the code at %s: %w", config.Issuer, err)
	}

	raw, ok := token.Extra("id_token").(string)
	if !ok || raw == "" {
		return nil, errNoIdentityToken
	}

	identity, err := provider.Verifier(&oidc.Config{ClientID: config.ClientID}).Verify(ctx, raw)
	if err != nil {
		return nil, fmt.Errorf("checking the identity token from %s: %w", config.Issuer, err)
	}

	if identity.Nonce != round.Nonce {
		return nil, errAnotherRound
	}

	var said struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}

	if err := identity.Claims(&said); err != nil {
		return nil, fmt.Errorf("reading the identity token from %s: %w", config.Issuer, err)
	}

	return &model.OpenIDClaims{
		Issuer: identity.Issuer, Subject: identity.Subject, Email: said.Email, Name: said.Name,
	}, nil
}

// Check reads a provider's discovery document, which is what the settings screen
// asks before a configuration is trusted with anybody's sign-in.
func (c *Client) Check(ctx context.Context, issuer string) error {
	_, err := c.provider(ctx, issuer)

	return err
}

// provider is a provider as its discovery document describes it, read once.
//
// A failed discovery is not kept, so a provider that was down is asked again by
// the next sign-in rather than remembered as broken.
func (c *Client) provider(ctx context.Context, issuer string) (*oidc.Provider, error) {
	c.mu.Lock()
	known, ok := c.discovered[issuer]
	c.mu.Unlock()

	if ok {
		return known, nil
	}

	// The client travels in the context because that is how the library is told
	// which one to use - for discovery here, and for the key set it keeps, which
	// is fetched with the client it was discovered with.
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, c.http), issuer)
	if err != nil {
		return nil, fmt.Errorf("reading the discovery document of %s: %w", issuer, err)
	}

	c.mu.Lock()
	c.discovered[issuer] = provider
	c.mu.Unlock()

	return provider, nil
}

// oauthConfig is the client as the provider registered it, for one round.
func oauthConfig(provider *oidc.Provider, config model.OpenIDConfig, round model.OpenIDRound) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     config.ClientID,
		ClientSecret: config.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  round.RedirectURI,
		Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
	}
}
