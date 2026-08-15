package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Which of two versions is newer.
//
// As numbers, not as text. "v0.1.10" sorts before "v0.1.9" as a string, and this
// project is already past that point - an installation compared the wrong way
// would be told it is current for the next ninety releases.
func TestWhichVersionIsNewer(t *testing.T) {
	for _, c := range []struct {
		have, want string
		newer      bool
	}{
		{"v0.1.9", "v0.1.10", true},
		{"v0.1.10", "v0.1.9", false},
		{"v0.1.27", "v0.1.29", true},
		{"v0.1.29", "v0.1.29", false},
		{"v0.9.9", "v1.0.0", true},
		{"v1.0.0", "v0.9.9", false},
		{"v1.2.3", "v1.3.0", true},

		// A build not made from a tag has nothing to compare, and an update
		// offered on the strength of a version nobody can read is worse than one
		// not offered.
		{"dev", "v1.0.0", false},
		{"v1.0.0", "dev", false},
		{"v1.0.0", "", false},
		{"v1.0", "v1.0.1", false},

		// A pre-release marker is not ranked, but the numbers before it are.
		{"v1.0.0", "v1.0.1-rc1", true},
	} {
		if got := Newer(c.have, c.want); got != c.newer {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.have, c.want, got, c.newer)
		}
	}
}

// The release feed, read into what the screen needs.
func TestReadingTheReleaseFeed(t *testing.T) {
	const version = "v9.9.9"

	binary := assetName(version)

	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": version,
			"body":     "what changed",
			"html_url": "https://example.invalid/release",
			"assets": []map[string]string{
				{"name": binary, "browser_download_url": "https://example.invalid/bin"},
				{"name": "SHA256SUMS", "browser_download_url": "https://example.invalid/sums"},
				{"name": "go-time-recording_v9.9.9_plan9_386", "browser_download_url": "nope"},
			},
		})
	}))
	defer feed.Close()

	release, err := New(feed.URL).Latest(context.Background())
	if err != nil {
		t.Fatalf("reading the feed: %v", err)
	}

	if release.Version != version {
		t.Errorf("read version %q", release.Version)
	}

	if !release.HasBinary() {
		t.Error("the release published this platform's binary and the checksums, " +
			"and neither was picked up")
	}
}

// A release with nothing for this platform is not one to offer.
func TestAReleaseWithoutThisPlatformIsNotOffered(t *testing.T) {
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "v9.9.9",
			"assets": []map[string]string{
				{"name": "SHA256SUMS", "browser_download_url": "https://example.invalid/sums"},
			},
		})
	}))
	defer feed.Close()

	release, err := New(feed.URL).Latest(context.Background())
	if err != nil {
		t.Fatalf("reading the feed: %v", err)
	}

	if release.HasBinary() {
		t.Error("a release that published nothing for this platform reads as " +
			"installable")
	}
}

// The download is checked against the published sum before it is written into
// place.
//
// This is code that will be executed as the application on the next start. A
// download that is merely "probably fine" is not a standard worth having, and
// the failure this guards against is not hypothetical: a proxy that serves an
// error page with a 200, a truncated body, a mirror nobody audited.
func TestADownloadThatDoesNotMatchIsRefused(t *testing.T) {
	const version = "v9.9.9"

	wanted := []byte("the real binary")
	other := []byte("something else entirely")

	sum := sha256.Sum256(wanted)

	for _, c := range []struct {
		name    string
		serve   []byte
		refused bool
	}{
		{"what the checksum says", wanted, false},
		{"anything else", other, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			source, release, target := stagedRelease(t, version, hex.EncodeToString(sum[:]), c.serve)

			err := source.download(context.Background(), release.asset, target,
				hex.EncodeToString(sum[:]))

			if c.refused && err == nil {
				t.Fatal("a download that does not match the published checksum was accepted")
			}

			if !c.refused && err != nil {
				t.Fatalf("a matching download was refused: %v", err)
			}
		})
	}
}

// A checksum file that does not mention this platform is a refusal, not a
// silently skipped check.
func TestMissingChecksumIsARefusal(t *testing.T) {
	source, release, _ := stagedRelease(t, "v9.9.9", "", []byte("x"))

	if _, err := source.checksum(context.Background(), release); err == nil {
		t.Error("a checksum file with no line for this platform was accepted")
	}
}

// stagedRelease serves a release's binary and checksums from a test server.
func stagedRelease(t *testing.T, version, sum string, body []byte) (*Source, Release, string) {
	t.Helper()

	name := assetName(version)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sums") {
			if sum == "" {
				// A file for some other platform only.
				_, _ = fmt.Fprintf(w, "%s  go-time-recording_%s_plan9_386\n",
					strings.Repeat("0", 64), version)

				return
			}

			_, _ = fmt.Fprintf(w, "%s  %s\n", sum, name)

			return
		}

		_, _ = w.Write(body)
	}))

	t.Cleanup(server.Close)

	return New(server.URL), Release{
		Version: version,
		asset:   server.URL + "/bin",
		sums:    server.URL + "/sums",
	}, filepath.Join(t.TempDir(), "staged")
}

// What is downloaded is executable, because it is about to be the application.
func TestTheDownloadIsExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits do not decide this on Windows")
	}

	body := []byte("#!/bin/sh\ntrue\n")
	sum := sha256.Sum256(body)

	source, release, target := stagedRelease(t, "v9.9.9", hex.EncodeToString(sum[:]), body)

	if err := source.download(context.Background(), release.asset, target,
		hex.EncodeToString(sum[:])); err != nil {
		t.Fatalf("downloading: %v", err)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}

	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("the downloaded binary is %v, which cannot be run", info.Mode().Perm())
	}
}
