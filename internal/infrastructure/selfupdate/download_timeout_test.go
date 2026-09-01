package selfupdate

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A slow connection is not a broken one, and the download must not treat it as
// one.
//
// Source.Client was given a ten-second timeout for the release lookup - a
// courtesy call on an administration screen, which is what its comment describes
// - and the binary download used the same client. http.Client.Timeout covers the
// whole exchange including the body, so that made ten seconds the budget for
// roughly thirty megabytes: about 24 Mbit/s sustained, before the redirect to the
// asset host and its own handshake are paid for out of the same ten. Below that
// the update fails every time, and it fails saying "the download broke off",
// which reads as a connection that dropped rather than a limit that was too
// small for the job.
//
// The update handler had already written down the right number and the wrong one
// was underneath it: "The download and its checks take tens of seconds."
//
// Nothing could have caught it. The suite serves the asset from localhost with a
// client that has no timeout at all, so the fast path is the only one it ever
// takes - which is the general shape of this: a timeout that is too short is
// invisible to every test that is fast.
//
// Bounded by progress instead of by duration. The lookup keeps its whole-exchange
// timeout, because a JSON document that has not arrived in ten seconds is not
// coming; the download gets a bound on the server answering at all, and is
// otherwise held by the caller's context and by maxDownload.
func TestASlowDownloadIsNotCutOffLikeAStalledOne(t *testing.T) {
	binary := workingProgram(t, "the new version")
	sum := sha256.Sum256(binary)

	// Short, so the case runs in under a second while standing for the real
	// arithmetic: a body that takes longer to arrive than the lookup's budget.
	const lookupBudget = 250 * time.Millisecond

	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "SHA256SUMS") {
			_, _ = fmt.Fprintf(w, "%x  %s\n", sum, assetName("v9.9.9"))

			return
		}

		// Steadily, in six pieces, never stalling - a connection that is simply
		// not fast. The whole body takes about twice the lookup budget.
		writeSlowly(w, binary, 6, lookupBudget/3)
	}))

	defer feed.Close()

	source := New(feed.URL, "")

	// The lookup client as production has it, only with its budget shrunk to
	// something a test can wait for. Before the fix this was also the download's
	// client, and that is the whole of the defect.
	source.Client = &http.Client{Timeout: lookupBudget}

	self := filepath.Join(t.TempDir(), "program"+exeSuffix())
	if err := os.WriteFile(self, []byte("the version now running"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := source.InstallOver(t.Context(), releaseOn(feed.URL), self)
	if err != nil {
		t.Fatalf("a download slower than the lookup budget was refused: %v", err)
	}

	installed, readErr := os.ReadFile(self)
	if readErr != nil {
		t.Fatalf("nothing at the installation's own path: %v", readErr)
	}

	if len(installed) != len(binary) {
		t.Errorf("the installed file is %d bytes and the release is %d",
			len(installed), len(binary))
	}
}

// And the bound is still a bound: a download that stops arriving is cut off.
//
// The fix removes a whole-exchange timeout, so this is the case that says what
// replaced it rather than nothing. A server that sends its headers and then stops
// is the failure that a response-header timeout cannot see, and the caller's
// context is what holds it - which is the honest answer, because a download whose
// bytes have stopped is indistinguishable from a slow one except by waiting.
func TestADownloadThatStopsArrivingIsStillCutOff(t *testing.T) {
	binary := workingProgram(t, "the new version")
	sum := sha256.Sum256(binary)

	stalled := make(chan struct{})

	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "SHA256SUMS") {
			_, _ = fmt.Fprintf(w, "%x  %s\n", sum, assetName("v9.9.9"))

			return
		}

		// Headers, one byte, and then nothing at all.
		w.Header().Set("Content-Length", fmt.Sprint(len(binary)))
		_, _ = w.Write(binary[:1])

		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}

		<-stalled
	}))

	t.Cleanup(func() { close(stalled); feed.Close() })

	source := New(feed.URL, "")

	self := filepath.Join(t.TempDir(), "program"+exeSuffix())
	if err := os.WriteFile(self, []byte("the version now running"), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()

	err := source.InstallOver(ctx, releaseOn(feed.URL), self)
	if err == nil {
		t.Fatal("a download that stopped arriving reported success")
	}

	if !errors.Is(err, context.DeadlineExceeded) &&
		!strings.Contains(err.Error(), "broke off") {
		t.Errorf("the refusal does not say the download did not finish: %v", err)
	}

	running, readErr := os.ReadFile(self)
	if readErr != nil || string(running) != "the version now running" {
		t.Errorf("a download that never finished disturbed the installed binary "+
			"(%q, %v)", running, readErr)
	}
}

// The lookup keeps its own whole-exchange bound.
//
// Removing the timeout from the download must not remove it from the call it was
// written for: a release feed that accepts a connection and never answers would
// otherwise hold an administration screen open indefinitely.
func TestTheReleaseLookupStillGivesUp(t *testing.T) {
	source := New("", "")

	if source.Client == nil || source.Client.Timeout == 0 {
		t.Fatal("the release lookup has no timeout; a feed that never answers " +
			"would hold the screen open")
	}

	if source.Downloader == nil {
		t.Fatal("the download has no client of its own, so it shares the lookup's " +
			"whole-exchange timeout again")
	}

	if source.Downloader.Timeout != 0 {
		t.Errorf("the download client has a whole-exchange timeout of %v, which "+
			"bounds a thirty-megabyte transfer by duration rather than by progress",
			source.Downloader.Timeout)
	}
}

// writeSlowly sends body in pieces, pausing between them.
func writeSlowly(w http.ResponseWriter, body []byte, pieces int, pause time.Duration) {
	w.Header().Set("Content-Length", fmt.Sprint(len(body)))

	size := (len(body) + pieces - 1) / pieces
	flusher, canFlush := w.(http.Flusher)

	for at := 0; at < len(body); at += size {
		if _, err := w.Write(body[at:min(at+size, len(body))]); err != nil {
			return
		}

		if canFlush {
			flusher.Flush()
		}

		time.Sleep(pause)
	}
}

// releaseOn is a release published by a test server.
func releaseOn(base string) Release {
	return Release{
		Version: "v9.9.9",
		asset:   base + "/" + assetName("v9.9.9"),
		sums:    base + "/SHA256SUMS",
	}
}
