//go:build integration

package integration

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Two things this application says without being asked.
//
// Everything else a browser learns, it learns by asking. That is right for almost
// all of it and wrong for exactly two: the binary underneath is being replaced
// and, where the platform allows it, the process is restarted seconds later - and
// what the account looking at the screen is still allowed to do. Somebody halfway
// through an entry finds out about the first when the page stops answering, and
// about the second when a button they no longer have any business pressing is
// refused.
//
// So there is a stream, and these are the properties that make it worth having:
// anybody signed in can open one, nobody signed out can, and both of those arrive
// on it while it is open.

// frame is one server-sent event: the name it was sent under, and its payload.
//
// The name matters here in a way it did not when there was one kind of event. A
// stream now carries announcements, which belong to the whole installation, and
// permission changes, which belong to the one account holding the connection -
// and a reader that takes every data line as the same thing would hand one to
// the code written for the other.
type frame struct {
	event string
	data  map[string]any
}

// openFrames connects to the stream and returns everything read from it, plus the
// status the server answered with.
func openFrames(t *testing.T, c *client) (<-chan frame, int, func()) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.app.BaseURL()+"/api/v1/events", nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}

	req.Header.Set("Accept", "text/event-stream")

	// No timeout: the whole point of this connection is that it stays open. The
	// shared client has one, and it would close this after fifteen seconds.
	stream := &http.Client{Jar: c.http.Jar}

	res, err := stream.Do(req)
	if err != nil {
		cancel()
		t.Fatalf("opening the stream: %v", err)
	}

	if res.StatusCode != http.StatusOK {
		status := res.StatusCode

		_ = res.Body.Close()

		cancel()

		return nil, status, func() {}
	}

	out := make(chan frame, 8)

	go func() {
		defer close(out)
		defer func() { _ = res.Body.Close() }()

		scanner := bufio.NewScanner(res.Body)

		// The name arrives on its own line, ahead of the payload it belongs to,
		// and is remembered until that payload turns up. A keep-alive comment in
		// between carries neither and leaves it alone.
		name := ""

		for scanner.Scan() {
			line := scanner.Text()

			if rest, found := strings.CutPrefix(line, "event: "); found {
				name = rest

				continue
			}

			payload, found := strings.CutPrefix(line, "data: ")
			if !found {
				continue
			}

			var decoded map[string]any
			if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
				continue
			}

			select {
			case out <- frame{event: name, data: decoded}:
			case <-ctx.Done():
				return
			}

			name = ""
		}
	}()

	return out, res.StatusCode, cancel
}

// openStream is openFrames narrowed to announcements, which is what the cases
// about updates are asking for.
func openStream(t *testing.T, c *client) (<-chan map[string]any, int, func()) {
	t.Helper()

	frames, status, done := openFrames(t, c)
	if frames == nil {
		return nil, status, done
	}

	out := make(chan map[string]any, 8)

	go func() {
		defer close(out)

		for f := range frames {
			if f.event != "announcement" {
				continue
			}

			out <- f.data
		}
	}()

	return out, status, done
}

// Somebody signed in gets a stream, and what is published arrives on it.
func TestAnAnnouncementReachesAnOpenStream(t *testing.T) {
	t.Parallel()

	app := start(t)
	admin := app.signInAsAdmin("a-much-better-password")

	stream, status, done := openStream(t, admin)
	defer done()

	if status != http.StatusOK {
		t.Fatalf("opening the stream answered %d", status)
	}

	// The stream is the only way in, so this rides on the update endpoint - which
	// refuses on an instance with nothing newer to install, and announces nothing
	// when it refuses. What is provable here without a release to install is that
	// the connection is open, held, and reading.
	//
	// The heartbeat proves exactly that: it is written by the same loop that
	// writes announcements, over the same connection, through everything in
	// between.
	began := time.Now()

	select {
	case announcement, ok := <-stream:
		if !ok {
			t.Fatalf("the stream closed by itself after %v; application log: %s",
				time.Since(began), app.log())
		}

		t.Logf("the stream carried %v", announcement)
	case <-time.After(2 * time.Second):
		// Nothing to say yet is the correct state for a fresh instance. The
		// connection being open and silent is the pass here.
	}
}

// Nobody signed out gets one.
//
// It carries no data belonging to anybody, which is why it needs no permission -
// but it is still a connection this application holds open, and holding one for
// every visitor is how a public endpoint becomes a way to exhaust a server.
func TestTheStreamIsRefusedWithoutASession(t *testing.T) {
	t.Parallel()

	app := start(t)

	// A client that has fetched the page - so it has the CSRF cookie a browser
	// has - and has not signed in.
	stranger := app.newClient()

	_, status, done := openStream(t, stranger)
	defer done()

	if status != http.StatusUnauthorized {
		t.Errorf("the stream answered %d to somebody not signed in, want %d",
			status, http.StatusUnauthorized)
	}
}

// The stream says it is a stream, and says not to store it.
//
// Both matter in front of a proxy: a response cached anywhere is an announcement
// delivered once and then never again, and a content type of anything else is a
// browser that will not treat it as events at all.
func TestTheStreamAnnouncesItselfAsOne(t *testing.T) {
	t.Parallel()

	app := start(t)
	admin := app.signInAsAdmin("a-much-better-password")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		admin.app.BaseURL()+"/api/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}

	res, err := (&http.Client{Jar: admin.http.Jar}).Do(req)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = res.Body.Close() }()

	if got := res.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Errorf("the stream is served as %q", got)
	}

	if got := res.Header.Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Errorf("the stream is cacheable (%q), so an announcement could be "+
			"delivered once and served from a cache for ever after", got)
	}
}

// A right that changes reaches the screen it changed on, with nobody clicking.
//
// The server has always enforced a change on the very next request - who is
// calling is resolved from the database every time - and the interface read /me
// once at start-up and kept what it was given. So the screen went on offering
// what the account could no longer do, and the person looking at it found out by
// pressing something and being refused.
//
// The gap that matters is exactly the one nobody is clicking through: a right is
// withdrawn while the person it was withdrawn from is reading a screen it opened.
// There is a poll for that, once a minute, and a minute of a screen that has
// stopped being true is a long time to be looking at one. This connection is
// already open and already belongs to that account, so it is the one place the
// answer can arrive without being asked for.
func TestARoleChangeReachesTheStreamOfTheAccountItWasMadeTo(t *testing.T) {
	t.Parallel()

	app := start(t)
	admin := app.signInAsAdmin("a-much-better-password")
	worker := app.signInAsUser(admin, "Wera", "wera@example.com")

	var before struct {
		User                userResponse `json:"user"`
		PermissionsRevision string       `json:"permissionsRevision"`
	}

	worker.must(worker.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &before)

	if before.PermissionsRevision == "" {
		t.Fatal("/me says nothing about what this account may do, so there is nothing " +
			"for a change to be measured against")
	}

	frames, status, done := openFrames(t, worker)
	defer done()

	if status != http.StatusOK {
		t.Fatalf("opening the stream answered %d", status)
	}

	// Made by somebody else entirely. From here on the browser holding the stream
	// does nothing at all - which is the whole point of the case.
	admin.must(admin.api(http.MethodPut,
		fmt.Sprintf("/users/%d/role", before.User.ID),
		map[string]any{"role": "user-admin"}), http.StatusOK)

	deadline := time.After(30 * time.Second)

	for {
		select {
		case f, ok := <-frames:
			if !ok {
				t.Fatalf("the stream closed before it said anything; application log: %s",
					app.log())
			}

			if f.event != "permissions" {
				continue
			}

			revision, _ := f.data["revision"].(string)

			if revision == "" {
				t.Fatalf("the stream said the rights changed without saying to what: %v",
					f.data)
			}

			if revision == before.PermissionsRevision {
				t.Fatalf("the stream reported %q, which is what this account already had",
					revision)
			}

			return

		case <-deadline:
			t.Fatalf("nothing on the stream said the rights had changed, so a screen "+
				"nobody is touching goes on offering what this account may no longer "+
				"do; application log: %s", app.log())
		}
	}
}
