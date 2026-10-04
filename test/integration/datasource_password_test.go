//go:build integration

package integration

import (
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appconfig "github.com/dennis-dko/go-time-recording/internal/infrastructure/config"
	"github.com/dennis-dko/go-time-recording/test/harness"
)

// A password left out of the form stands for the one belonging to the connection
// the form was filled from, and only goes to that server as that user.
//
// The screen never receives the stored password, so an untouched password field
// means "keep it". The connection test honoured that for any server it was
// pointed at: change the host or the user, leave the password alone, press
// "Test connection", and the stored password went to wherever the form now
// named. PostgreSQL lets a server ask for it in clear text and lib/pq sends it,
// so the one credential the screen is built never to show could be collected by
// pointing the test at a listener. The server here is that listener.
func TestTheKeptPasswordGoesOnlyToItsOwnServer(t *testing.T) {
	t.Parallel()

	port, passwords := passwordCatcher(t)

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	connection := func(user, password string) map[string]any {
		return map[string]any{
			"dialect": "postgres", "name": "gtr", "host": "127.0.0.1", "port": port,
			"user": user, "password": password, "sslMode": "disable",
		}
	}

	admin.must(admin.api(http.MethodPut, "/settings/datasource",
		connection("gtr", "the-stored-secret")), http.StatusOK)

	// Its own server and user: the kept password is what an untouched field means.
	admin.must(admin.api(http.MethodPost, "/settings/datasource/test", connection("gtr", "")),
		http.StatusOK, http.StatusCreated)

	if got := caught(t, passwords); got != "the-stored-secret" {
		t.Errorf("testing the stored connection unchanged sent %q, want the stored password", got)
	}

	// Another user on the same server is another account's password to know.
	//
	// So the box stays empty, and a PostgreSQL connection without a password is
	// refused before anything is dialled, because the driver would read it with
	// the database's name swallowed. The refusal is the evidence: had the stored
	// password been put in, the connection would have been read as typed and
	// dialled, and the listener would have caught it before the answer came back.
	var probed struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}

	admin.must(admin.api(http.MethodPost, "/settings/datasource/test", connection("someone", "")),
		http.StatusOK, http.StatusCreated).Data(t, &probed)

	if probed.Error.Code != "passwordSwallowsName" {
		t.Errorf("testing a connection for another user was answered %q, so the box was "+
			"not left empty", probed.Error.Code)
	}

	select {
	case got := <-passwords:
		t.Errorf("testing a connection for another user sent %q to it", got)
	default:
	}
}

// A connection from the environment keeps its password when the card is saved
// or tested as it stands.
//
// The card shows the connection in force as values, the environment's as much as
// a stored one, and its password as a filled box the page never receives. So an
// untouched box has to mean the password in force - which, with nothing stored,
// is the one the environment gave. It meant only a stored one: saving the card
// unchanged on a compose deployment wrote a connection with no password, which
// takes precedence over the environment at the next start, and that start could
// no longer sign in to its own database.
//
// Only a server has a password, so this runs where GTR_TEST_DSN names one - the
// PostgreSQL and MySQL legs.
func TestAnUntouchedPasswordKeepsTheOneTheEnvironmentGave(t *testing.T) {
	t.Parallel()

	dsn := os.Getenv(harness.DSNEnv)
	if dsn == "" {
		t.Skipf("%s names no database server, and SQLite has no password", harness.DSNEnv)
	}

	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("%s is not a URL: %v", harness.DSNEnv, err)
	}

	given, _ := parsed.User.Password()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	var shown struct {
		Stored             bool           `json:"stored"`
		RunningHasPassword bool           `json:"runningHasPassword"`
		Running            map[string]any `json:"running"`
	}

	admin.must(admin.api(http.MethodGet, "/settings/datasource", nil), http.StatusOK).Data(t, &shown)

	if shown.Stored {
		t.Fatal("the instance has a stored connection, so this case measures nothing")
	}

	if !shown.RunningHasPassword {
		t.Error("the card is not told the running connection has a password, so it cannot show one")
	}

	// The card as it stands: the connection in force, the password box untouched.
	untouched := shown.Running
	untouched["password"] = ""

	var probed struct {
		OK bool `json:"ok"`
	}

	admin.must(admin.api(http.MethodPost, "/settings/datasource/test", untouched),
		http.StatusOK, http.StatusCreated).Data(t, &probed)

	if !probed.OK {
		t.Error("testing the connection in force, untouched, did not sign in to it")
	}

	admin.must(admin.api(http.MethodPut, "/settings/datasource", untouched), http.StatusOK)

	saved, ok := appconfig.LoadDatasource(filepath.Join(a.Dir(), "configs", "datasource.json"))
	if !ok {
		t.Fatal("saving the card stored nothing")
	}

	if saved.Password != given {
		t.Errorf("the stored connection carries the password %q, want the one the environment gave, "+
			"or the next start cannot sign in to its database", saved.Password)
	}
}

// passwordCatcher is a PostgreSQL server that asks every client for its password
// in clear text and hands over what it is given, then refuses the sign-in.
func passwordCatcher(t *testing.T) (string, <-chan string) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}

	t.Cleanup(func() { _ = listener.Close() })

	passwords := make(chan string, 8)

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}

			go catchOne(conn, passwords)
		}
	}()

	_, port, _ := net.SplitHostPort(listener.Addr().String())

	return port, passwords
}

func catchOne(conn net.Conn, passwords chan<- string) {
	defer func() { _ = conn.Close() }()

	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	// The startup message, after any request for an encrypted channel, which
	// is declined so the conversation stays readable.
	for {
		body, ok := readMessage(conn)
		if !ok {
			return
		}

		if len(body) == 4 {
			if code := binary.BigEndian.Uint32(body); code == 80877103 || code == 80877104 {
				_, _ = conn.Write([]byte("N"))

				continue
			}
		}

		break
	}

	// AuthenticationCleartextPassword.
	_, _ = conn.Write([]byte{'R', 0, 0, 0, 8, 0, 0, 0, 3})

	var kind [1]byte
	if _, err := io.ReadFull(conn, kind[:]); err != nil || kind[0] != 'p' {
		return
	}

	body, ok := readMessage(conn)
	if !ok {
		return
	}

	passwords <- strings.TrimRight(string(body), "\x00")

	refusal := []byte("SFATAL\x00C28P01\x00Mpassword authentication failed\x00\x00")
	frame := binary.BigEndian.AppendUint32([]byte{'E'}, uint32(len(refusal)+4))
	_, _ = conn.Write(append(frame, refusal...))
}

// readMessage reads one length-prefixed PostgreSQL message body.
func readMessage(conn net.Conn) ([]byte, bool) {
	var length uint32
	if err := binary.Read(conn, binary.BigEndian, &length); err != nil || length < 4 || length > 1<<16 {
		return nil, false
	}

	body := make([]byte, length-4)
	if _, err := io.ReadFull(conn, body); err != nil {
		return nil, false
	}

	return body, true
}

// caught waits for the password the next probe hands over.
func caught(t *testing.T, passwords <-chan string) string {
	t.Helper()

	select {
	case got := <-passwords:
		return got
	case <-time.After(15 * time.Second):
		t.Fatal("the probe never reached the listener, so this case measures nothing")

		return ""
	}
}
