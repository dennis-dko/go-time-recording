// Package tlsserver obtains and renews Let's Encrypt certificates and serves
// the application over HTTPS.
//
// GoFr owns its own HTTP listener and only accepts a certificate file pair, so
// automatic certificates are handled here: this package terminates TLS and
// forwards to GoFr's plain listener on localhost.
package tlsserver
