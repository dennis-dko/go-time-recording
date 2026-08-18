package harness

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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

// An answer from the address is not proof it came from this instance.
//
// The port was free when it was asked for and is bound by a different process,
// so the address can belong to another test's instance that is still running -
// and that one answers 200 to anything. Accepting it means the test talks to
// somebody else's database, and the failure lands three steps later: an account
// it creates already exists, reported as a 409 nobody can explain.
//
// Our own process binds within moments of starting, so by the time anything
// answers it has either bound or said it could not - which is what makes looking
// again, after the answer, worth doing.
func TestAnAnswerIsCheckedAgainstWhatThisProcessSaid(t *testing.T) {
	lost := &App{logs: logBuffer(
		`{"level":"ERROR","message":"listen tcp :45247: bind: address already in use"}`)}

	if !errors.Is(lost.startupFailure(), errPortTaken) {
		t.Error("an instance that lost its port would be accepted as ready")
	}

	fine := &App{logs: logBuffer(
		`{"level":"WARN","message":"TLS_ENABLED is false"}`)}

	if fine.startupFailure() != nil {
		t.Errorf("an ordinary start is treated as a failure: %v", fine.startupFailure())
	}
}

// A reply carrying somebody else's name is somebody else's instance - and a
// reply that never came is not a reply at all.
//
// Reading the log was the first attempt at this and it is a race of its own: it
// only works if our process has already written that it could not bind by the
// time the neighbour answers, and the neighbour is already running, so it
// usually answers first. That is what one failed run of the suite on main looked
// like - an account that already existed, three steps after the harness had
// accepted a stranger's 200.
//
// The third answer is the one that cost a second run. The identity request has a
// short deadline and the suite runs several cases at once, so under load it is
// the first thing to time out; treating that as "ours" accepts whatever is on
// the port, which is the mix-up this exists to prevent.
func TestAReplyFromAnotherInstanceIsRecognised(t *testing.T) {
	answers := func(title string, status int) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = fmt.Fprintf(w, `{"data":{"title":%q}}`, title)
			}))
	}

	client := &http.Client{Timeout: 2 * time.Second}

	ours := answers("harness-1-7", http.StatusOK)
	defer ours.Close()

	mine := &App{baseURL: ours.URL, marker: "harness-1-7"}
	if got := mine.whoAnswered(client); got != answerIsOurs {
		t.Errorf("an instance does not recognise its own answer: %v", got)
	}

	theirs := answers("harness-1-8", http.StatusOK)
	defer theirs.Close()

	stranger := &App{baseURL: theirs.URL, marker: "harness-1-7"}
	if got := stranger.whoAnswered(client); got != answerIsAStranger {
		t.Errorf("a neighbour's answer is taken for this instance's (%v), which is "+
			"how a case comes to talk to somebody else's database", got)
	}

	// Nothing listening at all: not an answer, and not a reason to accept the
	// address either. This is what a loaded machine produces.
	silent := answers("harness-1-9", http.StatusOK)
	silent.Close()

	unanswered := &App{baseURL: silent.URL, marker: "harness-1-7"}
	if got := unanswered.whoAnswered(client); got != answerIsUnclear {
		t.Errorf("a request that never got an answer reads as %v", got)
	}

	// An instance that cannot be asked is not one that failed the question. The
	// installer runs before there is a database and does not register the route
	// that would answer; reading that as a stranger would leave it a port it
	// could never bind.
	unasked := answers("", http.StatusNotFound)
	defer unasked.Close()

	installer := &App{baseURL: unasked.URL, marker: "harness-1-7"}
	if got := installer.whoAnswered(client); got != answerIsOurs {
		t.Errorf("an instance that cannot answer the question reads as %v", got)
	}

	// And an instance the caller named itself carries no marker, so there is
	// nothing to compare and the old check is what it falls back to.
	named := &App{baseURL: theirs.URL}
	if got := named.whoAnswered(client); got != answerIsOurs {
		t.Errorf("an instance named by its test is judged against a name it never had")
	}
}

// A port is never suggested twice in one run.
//
// The operating system hands the same number out again once the listener is
// closed, and it does: a suite that starts dozens of instances asks dozens of
// times. Two instances on one port is the failure all of this guards against, so
// the cheapest half of the guard is not to suggest one twice.
func TestAPortIsHandedOutOnce(t *testing.T) {
	seen := map[int]bool{}

	for range 40 {
		port := FreePort(t)

		if seen[port] {
			t.Fatalf("port %d was handed out twice in one run", port)
		}

		seen[port] = true
	}
}
