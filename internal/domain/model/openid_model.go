package model

import (
	"strings"
	"time"
)

// SettingOpenID stores the OpenID Connect provider an installation lets people
// sign in through, as JSON with the client secret sealed.
const SettingOpenID = "openid.config"

// OpenIDConfig is the provider an installation lets people sign in through -
// Keycloak, Authentik, Entra ID, Google - by the authorization code flow.
//
// Administered at run time, as the directory is: an organisation without a
// directory of its own usually has one of these instead, and it is set up by
// whoever administers the installation rather than whoever deploys it.
type OpenIDConfig struct {
	Enabled bool

	// Issuer is the provider's address, which its discovery document is read
	// from and which every token it signs has to name exactly.
	Issuer string

	// ClientID and ClientSecret are what the provider registered this
	// installation as. The secret is sealed with SECRET_KEY where it is stored, as
	// the directory's bind password is.
	ClientID     string
	ClientSecret string

	// Label names the provider on the sign-in form, "Sign in with" it.
	Label string
}

// Offered reports whether the sign-in form can offer the provider: switched on,
// and enough of it filled in to send somebody there.
func (c OpenIDConfig) Offered() bool {
	return c.Enabled && strings.TrimSpace(c.Issuer) != "" && strings.TrimSpace(c.ClientID) != ""
}

// OpenIDIdentity links an account to who a provider says somebody is: the
// provider's issuer and the subject it gives that person.
//
// The subject rather than an address, because the subject is the one thing a
// provider promises never to change and never to give to anybody else, while an
// address can be changed, reused, or - at some providers - claimed without being
// verified. And linked by the account's owner while signed in, never matched on
// arrival: an identity nobody linked opens nothing.
type OpenIDIdentity struct {
	ID        uint
	UserID    uint
	Issuer    string
	Subject   string
	CreatedAt time.Time
}

// OpenIDRound is one round trip through the provider: what is made before the
// browser leaves and checked when it comes back.
type OpenIDRound struct {
	// State is handed to the provider and comes back unchanged, which is how the
	// answer is matched to the question.
	State string

	// Nonce is signed into the identity token, so a token minted for another
	// round is refused.
	Nonce string

	// Verifier is the PKCE secret, so a code intercepted on its way back cannot be
	// exchanged by anybody else.
	Verifier string

	// RedirectURI is where the provider sends the browser back, which the token
	// exchange has to name again exactly.
	RedirectURI string
}

// OpenIDClaims is who the provider says came back.
type OpenIDClaims struct {
	Issuer  string
	Subject string
	Email   string
	Name    string
}
