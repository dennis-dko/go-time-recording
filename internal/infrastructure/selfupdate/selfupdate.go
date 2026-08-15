// Package selfupdate finds out whether a newer release exists, and where it can,
// installs it.
//
// # Why "where it can"
//
// This ships four ways, and only one of them can be updated from inside the
// running application. A single binary can fetch its successor, prove it is the
// one the release published, and put it in its own place. A container cannot: it
// has no business talking to the runtime that started it, and a binary swapped
// inside a container is undone by the next `docker compose up` - which is the
// one moment somebody is certain the update took. Offering a button there would
// be offering an update that silently reverts.
//
// So the check runs everywhere and the install does not. Where it does not, the
// screen says what to run instead. That is not a gap in the feature; it is what
// updating a container is.
//
// # What is verified
//
// The release publishes a SHA256SUMS beside its binaries. Nothing downloaded here
// is written into place until it hashes to what that file says, and the file is
// read from the same release as the binary. This is code that will be executed as
// the application on the next start: a download that is merely "probably fine" is
// not a standard worth having.
package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Release is what the newest published version looks like from here.
type Release struct {
	// Version is the tag, "v1.2.3".
	Version string

	// Notes is what the release says about itself, for the screen offering it.
	Notes string

	// URL is where a person can read the release rather than take our word.
	URL string

	// asset is the download for this platform, empty when the release has none -
	// a platform nobody published for cannot be updated to.
	asset string

	// sums is the SHA256SUMS published beside it.
	sums string
}

// HasBinary reports whether this release published something for this platform.
func (r Release) HasBinary() bool { return r.asset != "" && r.sums != "" }

// Source is where releases are looked up. Its own type so a test can answer
// without a network, and so an installation can point it at a mirror.
type Source struct {
	// API is the releases endpoint. GitHub's by default.
	API string

	// Client is who does the asking. Given a short timeout by New, because this
	// runs inside a request somebody is waiting on.
	Client *http.Client
}

// DefaultAPI is where the releases of this project live.
const DefaultAPI = "https://api.github.com/repos/dennis-dko/go-time-recording/releases/latest"

// New creates a source with sensible limits.
func New(api string) *Source {
	if strings.TrimSpace(api) == "" {
		api = DefaultAPI
	}

	return &Source{
		API: api,

		// Short, and no keep-alive worth speaking of: this is a courtesy lookup
		// on an administration screen, not something the application needs.
		Client: &http.Client{Timeout: 10 * time.Second},
	}
}

// assetName is what this platform's binary is called in a release.
//
// The same shape the release workflow builds, and windows is the one that
// differs - it carries the extension the operating system needs to run it at
// all.
func assetName(version string) string {
	name := fmt.Sprintf("go-time-recording_%s_%s_%s", version, runtime.GOOS, runtime.GOARCH)

	if runtime.GOOS == "windows" {
		name += ".exe"
	}

	return name
}

// Latest asks what the newest release is.
func (s *Source) Latest(ctx context.Context) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.API, nil)
	if err != nil {
		return Release{}, err
	}

	req.Header.Set("Accept", "application/vnd.github+json")

	res, err := s.Client.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("cannot reach the release feed: %w", err)
	}

	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("the release feed answered %d", res.StatusCode)
	}

	var body struct {
		TagName string `json:"tag_name"`
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}

	// Bounded, because this is a body from somewhere else and a release note has
	// no business being megabytes.
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&body); err != nil {
		return Release{}, fmt.Errorf("the release feed is not readable: %w", err)
	}

	release := Release{Version: body.TagName, Notes: body.Body, URL: body.HTMLURL}
	wanted := assetName(body.TagName)

	for _, asset := range body.Assets {
		switch asset.Name {
		case wanted:
			release.asset = asset.URL
		case "SHA256SUMS":
			release.sums = asset.URL
		}
	}

	return release, nil
}

// Newer reports whether want is a later version than have.
//
// Compared field by field as numbers rather than as text, because "v0.1.10" sorts
// before "v0.1.9" as a string and an installation would be told it is up to date
// for the next ninety releases. Anything unparseable is treated as not newer: an
// update offered on the strength of a version nobody can read is worse than one
// not offered.
func Newer(have, want string) bool {
	mine, ok := parseVersion(have)
	if !ok {
		return false
	}

	theirs, ok := parseVersion(want)
	if !ok {
		return false
	}

	for i := range theirs {
		if theirs[i] != mine[i] {
			return theirs[i] > mine[i]
		}
	}

	return false
}

// Comparable reports whether a version can be ranked at all.
//
// A build not made from a tag calls itself "dev", and "dev" is neither newer nor
// older than anything. The screen needs that as its own answer: told only that
// nothing newer exists, it would say "dev is the newest version", which is not
// true and not useful.
func Comparable(version string) bool {
	_, ok := parseVersion(version)

	return ok
}

// parseVersion reads "v1.2.3" into its three numbers.
func parseVersion(raw string) ([3]int, bool) {
	var out [3]int

	trimmed := strings.TrimPrefix(strings.TrimSpace(raw), "v")

	// A build that was not made from a tag calls itself "dev", and there is
	// nothing to compare that against.
	parts := strings.Split(trimmed, ".")
	if len(parts) != 3 {
		return out, false
	}

	for i, part := range parts {
		// Anything after a dash is a pre-release marker, which this does not rank.
		number, _, _ := strings.Cut(part, "-")

		value, err := strconv.Atoi(number)
		if err != nil || value < 0 {
			return out, false
		}

		out[i] = value
	}

	return out, true
}

// InContainer reports whether this process is running inside one.
//
// Which decides whether installing is offered at all. Two signals, because
// neither is universal: the file Docker leaves behind, and the container runtime
// in this process's own cgroup - the second catches podman and a plain
// containerd, the first catches a Docker container whose cgroup has been
// namespaced away.
func InContainer() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}

	cgroup, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return false
	}

	text := string(cgroup)

	return strings.Contains(text, "docker") || strings.Contains(text, "containerd") ||
		strings.Contains(text, "kubepods")
}

// Install downloads the release and puts it where this process's binary is.
//
// It does not restart. Replacing the file and replacing the process are separate
// acts with separate failure modes, and on Windows the second one is not
// available at all - so the caller decides what to do once the bytes are in
// place, and the screen says which of the two it is.
func (s *Source) Install(ctx context.Context, release Release) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot find this program's own file: %w", err)
	}

	// Resolved, so replacing a symlinked binary replaces the binary rather than
	// the link to it.
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}

	return s.InstallOver(ctx, release, self)
}

// InstallOver is Install against a named file.
//
// Separate from Install so the download, the checksum and the swap can be driven
// by a test - the riskiest thing this application does is replace its own binary,
// and a test that had to replace the test binary to check it would be worse than
// no test. Install is then the one line that says which file that is.
func (s *Source) InstallOver(ctx context.Context, release Release, self string) error {
	if !release.HasBinary() {
		return fmt.Errorf("release %s published nothing for %s/%s",
			release.Version, runtime.GOOS, runtime.GOARCH)
	}

	want, err := s.checksum(ctx, release)
	if err != nil {
		return err
	}

	// Beside the binary rather than in a temporary directory: the move at the end
	// has to be a rename, and a rename across filesystems is not one. /tmp on its
	// own mount is the ordinary case, not the exception.
	staged := self + ".new"
	pendingVersion = release.Version

	if err := s.download(ctx, release.asset, staged, want); err != nil {
		_ = os.Remove(staged)

		return err
	}

	return swap(self, staged)
}

// checksum reads the published hash for this platform's asset.
func (s *Source) checksum(ctx context.Context, release Release) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, release.sums, nil)
	if err != nil {
		return "", err
	}

	res, err := s.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("cannot read the published checksums: %w", err)
	}

	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the checksums answered %d", res.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<16))
	if err != nil {
		return "", err
	}

	wanted := assetName(release.Version)

	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}

		// sha256sum writes "hash  name" and marks a binary read with a leading
		// asterisk on the name.
		if strings.TrimPrefix(fields[1], "*") == wanted {
			return strings.ToLower(fields[0]), nil
		}
	}

	return "", fmt.Errorf("the published checksums do not mention %s", wanted)
}

// download fetches the asset and writes it only if it hashes to want.
func (s *Source) download(ctx context.Context, url, into, want string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	res, err := s.Client.Do(req)
	if err != nil {
		return fmt.Errorf("cannot download the release: %w", err)
	}

	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("the download answered %d", res.StatusCode)
	}

	// 0o755 rather than 0o644: this is about to be the application.
	file, err := os.OpenFile(into, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("cannot write beside the current binary: %w", err)
	}

	sum := sha256.New()

	// Hashed while it is written rather than read back afterwards, so the bytes
	// that were checked are the bytes that landed.
	if _, err := io.Copy(io.MultiWriter(file, sum), io.LimitReader(res.Body, maxDownload)); err != nil {
		_ = file.Close()

		return fmt.Errorf("the download broke off: %w", err)
	}

	if err := file.Close(); err != nil {
		return err
	}

	if got := hex.EncodeToString(sum.Sum(nil)); got != want {
		return fmt.Errorf("the download does not match the published checksum "+
			"(got %s, expected %s)", got[:16], want[:16])
	}

	return nil
}

// maxDownload bounds what will be written to disk. The published binaries are
// around thirty megabytes; a hundred is room to grow and still a bound.
const maxDownload = 100 << 20

// Installed reports the version of a staged update that is waiting for a
// restart, if there is one.
//
// This is what lets the screen say "downloaded, restart to use it" rather than
// offering the same update again to somebody who has already taken it.
func Installed() (string, bool) {
	self, err := os.Executable()
	if err != nil {
		return "", false
	}

	raw, err := os.ReadFile(self + ".pending")
	if err != nil {
		return "", false
	}

	return strings.TrimSpace(string(raw)), true
}

// markPending records which version is now on disk but not yet running.
//
// So the screen can say "downloaded, restart to use it" rather than offering the
// same update again to somebody who has already taken it - which on Windows,
// where the restart is somebody walking over to the machine, may be a while.
func markPending(self string) error {
	// Best effort. The update itself has succeeded by this point, and failing it
	// over a note would be undoing something that worked for the sake of the
	// message about it.
	_ = os.WriteFile(self+".pending", []byte(pendingVersion), 0o644)

	return nil
}

// pendingVersion is set by Install before the swap, so markPending has something
// to write without threading it through the platform-specific half.
var pendingVersion string

// Cleanup removes what a previous update left behind, once this process is the
// new version. Called at start-up.
func Cleanup() {
	self, err := os.Executable()
	if err != nil {
		return
	}

	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}

	removeLeftovers(self)
}
