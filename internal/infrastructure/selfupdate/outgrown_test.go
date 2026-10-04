package selfupdate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// A release that has outgrown what this version downloads is refused as that,
// and not as a download that failed its checksum.
//
// The bound is compiled into the version that is running, so the installations
// that meet a release larger than it are the ones already out there - all of
// them, on the same day. The download was cut at the bound and then hashed, so
// what they were told was that it "does not match the published checksum": a
// sentence about tampering or a broken mirror, for a file that arrived exactly
// as it was published, and nothing about the one thing that helps - that this
// version cannot take the release and the binary has to be replaced by hand.
func TestAReleaseLargerThanTheBoundIsRefusedAsThat(t *testing.T) {
	body := bytes.Repeat([]byte("x"), 4096)
	sum := sha256.Sum256(body)
	want := hex.EncodeToString(sum[:])

	for _, c := range []struct {
		name     string
		declared bool
	}{
		// Refused on the header, before a byte of the body is fetched.
		{"a length the server declares", true},

		// A chunked answer says nothing in advance, so this one is only known
		// once more has arrived than fits.
		{"a length it does not", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if c.declared {
					w.Header().Set("Content-Length", strconv.Itoa(len(body)))
				} else {
					w.(http.Flusher).Flush()
				}

				_, _ = w.Write(body)
			}))

			t.Cleanup(server.Close)

			source := New(server.URL, "")
			source.limit = 1024

			staged := filepath.Join(t.TempDir(), "staged")

			err := source.download(context.Background(), server.URL, staged, want)
			if err == nil {
				t.Fatal("a download four times the bound was accepted")
			}

			if strings.Contains(err.Error(), "checksum") {
				t.Errorf("a release larger than the bound is reported as a checksum that does not match: %v", err)
			}

			if !strings.Contains(err.Error(), "larger than") {
				t.Errorf("the refusal does not say the release is larger than this version downloads: %v", err)
			}

			// What the header already said is not fetched to be found out again.
			if _, statErr := os.Stat(staged); c.declared && statErr == nil {
				t.Error("a release that declared itself too large was downloaded before it was refused")
			}
		})
	}
}
