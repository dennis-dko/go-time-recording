//go:build integration

package integration

import (
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
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
	admin.must(admin.api(http.MethodPost, "/settings/datasource/test", connection("someone", "")),
		http.StatusOK, http.StatusCreated)

	if got := caught(t, passwords); got == "the-stored-secret" {
		t.Error("testing a connection for another user sent the stored password to it")
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
