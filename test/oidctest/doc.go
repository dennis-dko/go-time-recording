// Package oidctest is an OpenID Connect provider for the tests, without a real one.
//
// What a provider does in a sign-in is small and checkable: it says where it is
// and which keys it signs with, sends the browser back with a code, and trades
// that code - with the PKCE verifier proving it is the same client asking - for an
// identity token it signs. Done here with a key made per provider, the package
// that talks to providers, the integration suite and the browser suite all go
// through a real code flow and verify a real signature, and none of them needs
// Keycloak running. One place, because three copies of how a token is made would
// be three chances to test against a token no provider sends.
package oidctest
