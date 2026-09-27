// Package directory authenticates users against an LDAP directory.
//
// It implements service.ExternalAuthenticator, so the application layer never
// sees the LDAP client and can be tested without a directory.
package directory
