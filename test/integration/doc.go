// Package integration exercises the application the way a browser and a script
// do: over HTTP, against a real database, through the real binary.
//
// The unit tests already cover the rules in isolation. What they cannot cover
// is everything between a request arriving and a rule being reached - the
// middleware order, the CSRF check, the session cookie, the migrations, the
// embedded assets, the wiring in main.go. Every bug found in this project by
// running it rather than testing it lived in exactly that gap: a login screen
// that could not be dismissed, a booking dated in UTC, a directory that could
// take over the administrator account.
//
// So this starts the compiled binary as a subprocess and talks to it. Nothing
// is stubbed.
//
//	go test -tags integration ./test/integration
//	GTR_TEST_DSN=postgres://... go test -tags integration ./test/integration
//	GTR_TEST_DSN=mysql://...    go test -tags integration ./test/integration
//
// The account in that DSN has to be allowed to CREATE DATABASE, because each
// test gets its own. On PostgreSQL the owner already can; on MySQL an ordinary
// user cannot, so use root there - it is a throwaway server either way.
package integration
