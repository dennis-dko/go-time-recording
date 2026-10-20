// Package openid signs people in through an OpenID Connect provider - Keycloak,
// Authentik, Entra ID, Google - by the authorization code flow, with PKCE and a
// nonce.
//
// It answers two questions and decides nothing: where to send a browser, and who
// the provider says came back. Which account that is, and whether they may come
// in, is the application's to decide, and so is everything a round trip has to
// remember between the two halves - the state, the nonce and the verifier arrive
// here and are never kept here.
//
// What it does keep is each provider it has discovered, because discovery is a
// request to the provider and its answer does not change between sign-ins. The
// provider's signing keys are fetched again by the library whenever a token names
// one it does not know, which is how a key the provider rotated is picked up
// without a restart.
package openid
