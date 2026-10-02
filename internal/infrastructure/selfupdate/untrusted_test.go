package selfupdate

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dennis-dko/go-time-recording/test/tempdir"
)

// A published checksum that is not one is refused, before anything is fetched
// and without taking the request down.
//
// The line for this platform was taken as it stood: lowercased, and compared.
// What it was compared with is sixty-four hexadecimal digits, so anything else
// could only ever mismatch - after the whole binary had been downloaded to find
// that out - and the sentence reporting the mismatch quoted the first sixteen
// characters of each side by slicing them. A published value shorter than that
// was a slice out of range: the install panicked, and the notice telling every
// open screen that a version was being installed was never taken back, because
// the path that takes it back is the one an error returns on.
func TestAPublishedChecksumThatIsNotOneIsRefusedBeforeAnythingIsFetched(t *testing.T) {
	const version = "v9.9.9"

	for name, published := range map[string]string{
		"shorter than a hash": "abc",
		"longer than a hash":  strings.Repeat("a", 65),
		"not hexadecimal":     strings.Repeat("z", 64),
	} {
		t.Run(name, func(t *testing.T) {
			var fetched atomic.Int32

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/sums") {
					_, _ = fmt.Fprintf(w, "%s  %s\n", published, assetName(version))

					return
				}

				fetched.Add(1)

				_, _ = w.Write([]byte("the binary"))
			}))
			defer server.Close()

			self := filepath.Join(tempdir.New(t), "go-time-recording"+exeSuffix())
			if err := os.WriteFile(self, []byte("the version running"), 0o755); err != nil {
				t.Fatal(err)
			}

			release := Release{Version: version, asset: server.URL + "/bin", sums: server.URL + "/sums"}

			// As the handler calls it. A panic here is the request ending as a stack
			// trace, which is reported as what it is rather than left to end the run.
			err := func() (err error) {
				defer func() {
					if r := recover(); r != nil {
						err = fmt.Errorf("the install panicked: %v", r)
					}
				}()

				return New(server.URL, "").InstallOver(context.Background(), release, self)
			}()

			if err == nil {
				t.Fatal("an install was accepted against a checksum that is not one")
			}

			if strings.Contains(err.Error(), "panicked") {
				t.Fatalf("%v", err)
			}

			if !strings.Contains(err.Error(), "checksum") {
				t.Errorf("the refusal does not say what is wrong with the release: %v", err)
			}

			if n := fetched.Load(); n != 0 {
				t.Errorf("the binary was fetched %d time(s) to be compared with a value "+
					"it could never match", n)
			}
		})
	}
}

// A download the last process did not live to finish does not stay beside the
// binary.
//
// An install removes what it staged on every way out, and a process that is
// stopped mid-download has no way out: the file stayed, executable and never
// checked against anything, until the next install happened to write over it.
// No download can be under way while the program is starting, so the start is
// where it goes.
func TestADownloadTheLastProcessDidNotFinishIsRemovedAtStart(t *testing.T) {
	self := filepath.Join(tempdir.New(t), "go-time-recording"+exeSuffix())

	for name, body := range map[string]string{
		"":     "the version running",
		".new": "half of a download",
		".old": "the version before it",
	} {
		if err := os.WriteFile(self+name, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	removeLeftovers(self)

	if _, err := os.Stat(self + ".new"); err == nil {
		t.Error("an unfinished download is still beside the binary after a start")
	}

	if _, err := os.Stat(self + ".old"); err != nil {
		t.Error("the start threw away the version to go back to")
	}
}
