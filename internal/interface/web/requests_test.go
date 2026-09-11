package web_test

import (
	"strings"
	"testing"
)

// Only reads are given up on.
//
// Aborting a request in the browser does not stop the server, which finishes the
// work either way. A timeout on a write would therefore report a failure for
// something that succeeded, and the obvious response to that message is to do it
// again - which for an import means writing every row twice.
//
// The distinction lives in one expression, and nothing about it is self-evident
// from reading the call: a later hand tidying "why is this only for safe methods"
// would take the guard rails off a lorry.
func TestOnlyReadRequestsAreGivenUpOn(t *testing.T) {
	js := asset(t, "/app.js")

	const gate = "SAFE_METHODS.has(method) && !options.signal ? new AbortController() : null"

	if !strings.Contains(js, gate) {
		t.Error("the request timeout is no longer limited to safe methods and to calls " +
			"that brought no signal of their own; a write aborted here reports a " +
			"failure for work the server has already done")
	}

	// The timer has to be cleared however the request ends, or a fast one leaves a
	// pending abort behind for a controller nobody is waiting on any more.
	if !strings.Contains(js, "clearTimeout(countdown)") {
		t.Error("the timeout timer is not cleared, so every request leaves one pending")
	}

	// And only our own abort may be reported as a timeout: a caller's signal firing
	// is their business, and "the server did not answer" would be a lie about it.
	if !strings.Contains(js, "if (giveUp?.signal.aborted)") {
		t.Error("any abort is now reported as a timeout, including one a caller asked " +
			"for itself")
	}
}
