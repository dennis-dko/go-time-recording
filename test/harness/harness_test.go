package harness

import (
	"errors"
	"net"
	"testing"
)

// A port that is taken between choosing it and binding it is tried again.
//
// FreePort asks the operating system for one, closes the listener and hands the
// number over, so there is a window in which anything else on the machine can
// take it. Nothing here can close that window - a port cannot be reserved and
// passed to a child process - so the answer is to notice and try another.
//
// Left unnoticed it costs a whole start-up deadline and reports itself as a slow
// machine: the application says "address already in use" once, listens to
// nothing, and the test waits sixty seconds for an instance that was never going
// to answer. That is what took a run of the suite on main.
//
// This is the recognition, which is the part that can be tested without racing
// anything: the same sentence the application writes when it loses.
func TestLosingThePortIsRecognised(t *testing.T) {
	app := &App{logs: logBuffer(
		`{"level":"ERROR","message":"error while listening to http server, ` +
			`err: listen tcp :45247: bind: address already in use"}`)}

	if !errors.Is(app.startupFailure(), errPortTaken) {
		t.Errorf("losing the port reads as %v, so the start is not tried again",
			app.startupFailure())
	}
}

// Anything else in the log is not that, and must not be retried into a
// misleading failure three attempts later.
func TestOtherStartupTroubleIsNotMistakenForIt(t *testing.T) {
	app := &App{logs: logBuffer(
		`{"level":"ERROR","message":"cannot open the database: permission denied"}`)}

	if errors.Is(app.startupFailure(), errPortTaken) {
		t.Error("a database that cannot be opened is being retried as a busy port")
	}
}

// And the port this hands out is one somebody can actually listen on, which is
// the whole of what it promises.
func TestFreePortIsFree(t *testing.T) {
	port := FreePort(t)

	listener, err := net.ListenTCP("tcp", &net.TCPAddr{Port: port})
	if err != nil {
		t.Fatalf("the port handed out cannot be listened on: %v", err)
	}

	_ = listener.Close()
}
