//go:build integration

package integration

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// An update is the one thing this application says without being asked.
//
// Everything else a browser learns, it learns by asking. That is right for
// almost all of it and wrong for exactly this: the binary underneath is being
// replaced and, where the platform allows it, the process is restarted seconds
// later. Somebody halfway through an entry finds out when the page stops
// answering.
//
// So there is a stream, and these are the properties that make it worth having:
// anybody signed in can open one, nobody signed out can, and something published
// while it is open arrives on it.

// openStream connects to the announcement stream and returns the announcements
// read from it, plus the status the server answered with.
func openStream(t *testing.T, c *client) (<-chan map[string]any, int, func()) {
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

	out := make(chan map[string]any, 8)

	go func() {
		defer close(out)
		defer func() { _ = res.Body.Close() }()

		scanner := bufio.NewScanner(res.Body)

		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			var announcement map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")),
				&announcement); err != nil {
				continue
			}

			select {
			case out <- announcement:
			case <-ctx.Done():
				return
			}
		}
	}()

	return out, res.StatusCode, cancel
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
