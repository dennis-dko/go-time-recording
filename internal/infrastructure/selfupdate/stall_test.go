package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A download that stops arriving is given up, and one that is merely slow is not.
//
// The install runs on the request that asked for it, and nothing ends that
// request while somebody waits: GoFr sets no deadline without REQUEST_TIMEOUT,
// and the browser never abandons a write. So a server that sent its headers and
// then fell silent held the install, and the banner announcing it, for as long
// as the tab stayed open. What bounds it is progress - nothing arriving for a
// while - and a slow download that keeps arriving must still finish, or the
// bound would be the whole-exchange timeout the Downloader exists to avoid.
func TestADownloadThatStopsArrivingIsGivenUp(t *testing.T) {
	quiet := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			for range 8 {
				_, _ = w.Write([]byte("slow but steady "))
				w.(http.Flusher).Flush()

				time.Sleep(100 * time.Millisecond)
			}

			return
		}

		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("the first bytes"))
		w.(http.Flusher).Flush()

		select {
		case <-quiet:
		case <-r.Context().Done():
		}
	}))

	t.Cleanup(func() {
		close(quiet)
		server.Close()
	})

	source := New(server.URL, "")
	source.stall = 300 * time.Millisecond

	// Eight writes a tenth of a second apart: longer than the stall bound in all,
	// never that long between two of them.
	steady := sha256.Sum256([]byte(strings.Repeat("slow but steady ", 8)))

	if err := source.download(context.Background(), server.URL+"/slow",
		filepath.Join(t.TempDir(), "slow"), hex.EncodeToString(steady[:])); err != nil {
		t.Fatalf("a download that kept arriving was given up: %v", err)
	}

	done := make(chan error, 1)

	go func() {
		done <- source.download(context.Background(), server.URL+"/silent",
			filepath.Join(t.TempDir(), "silent"), "whatever it would have been")
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a download that stopped arriving was reported as complete")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the download was still waiting after ten seconds on a server that " +
			"stopped sending, with nothing to end it")
	}
}
