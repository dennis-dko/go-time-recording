//go:build integration

package integration

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"
)

// A stop waits for the requests it finds under way.
//
// The operations guide promises it - the application "waits for requests in
// flight on shutdown", for SHUTDOWN_GRACE_PERIOD - and GoFr's shutdown does
// wait, inside http.Server.Shutdown. But the server's ListenAndServe returns the
// moment that shutdown begins, GoFr's Run returns when it does, and main ends
// when Run does: the process could be gone while the shutdown was still
// waiting. A booking being saved at that moment is answered by nothing, and
// whoever sent it is left to guess whether it was stored.
//
// So the request here is one whose body is still arriving when the stop does,
// which keeps it under way for as long as the case chooses.
func TestAStopWaitsForARequestAlreadyUnderWay(t *testing.T) {
	t.Parallel()

	a := start(t, "METRICS_PORT=0")
	c := a.newClient()

	body, feed := io.Pipe()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		a.BaseURL()+"/api/v1/auth/login", body)
	if err != nil {
		t.Fatal(err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", a.BaseURL())
	req.Header.Set("X-CSRF-Token", c.csrfToken())

	answered := make(chan error, 1)

	go func() {
		resp, err := c.http.Do(req)
		if err != nil {
			answered <- err

			return
		}

		defer func() { _ = resp.Body.Close() }()

		if _, err := io.ReadAll(resp.Body); err != nil {
			answered <- err

			return
		}

		if resp.StatusCode != http.StatusUnauthorized {
			answered <- fmt.Errorf("answered %d, want the 401 a wrong password gets", resp.StatusCode)

			return
		}

		answered <- nil
	}()

	// A pipe hands its bytes over only when they are read, so once this returns
	// the request line and headers have gone and the body has begun.
	if _, err := feed.Write([]byte(`{"email":"nobody@example.com",`)); err != nil {
		t.Fatal(err)
	}

	// And the server has had its moment to take the request up.
	time.Sleep(300 * time.Millisecond)

	if err := a.Terminate(); err != nil {
		t.Skipf("this platform cannot send the instance a stop signal: %v", err)
	}

	// Long enough for a process that does not wait to be gone.
	time.Sleep(time.Second)

	_, _ = feed.Write([]byte(`"password":"not-the-password"}`))
	_ = feed.Close()

	select {
	case err := <-answered:
		if err != nil {
			t.Errorf("a request under way when the stop came was not answered: %v\n\n%s",
				err, truncate(a.log(), 1500))
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the request under way was neither answered nor cut off")
	}
}
