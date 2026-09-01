package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
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

	release, err := New(feed.URL, "").Latest(context.Background())
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

	release, err := New(feed.URL, "").Latest(context.Background())
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

	return New(server.URL, ""), Release{
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

// The whole of an install: download, check, and put it where the old one was.
//
// The riskiest thing this application does. A swap that half works leaves an
// installation with no binary at its own path, which is the one state from which
// nothing on the administration screen can help - so this drives it end to end
// against a file that is not the test binary.
func TestAnInstallReplacesTheFileItWasGiven(t *testing.T) {
	const version = "v9.9.9"

	newBinary := workingProgram(t, "the new version")
	sum := sha256.Sum256(newBinary)

	source, release, _ := stagedRelease(t, version, hex.EncodeToString(sum[:]), newBinary)

	self := filepath.Join(t.TempDir(), "go-time-recording"+exeSuffix())

	if err := os.WriteFile(self, []byte("the old version"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := source.InstallOver(context.Background(), release, self); err != nil {
		t.Fatalf("installing: %v", err)
	}

	got, err := os.ReadFile(self)
	if err != nil {
		t.Fatalf("the binary is not at its own path any more: %v", err)
	}

	if string(got) != string(newBinary) {
		t.Error("the file does not hold the downloaded bytes after the update")
	}

	// The version it replaced, kept so there is something to go back to. Losing
	// it is the failure the whole arrangement is shaped around: an update that
	// installs and then will not serve leaves no screen to press a button on.
	if _, err := os.Stat(self + ".old"); err != nil {
		t.Error("the previous binary was not kept, so a rollback is impossible")
	}

	// The staging file is gone rather than left beside the binary.
	if _, err := os.Stat(self + ".new"); err == nil {
		t.Error("the staged download is still lying beside the binary")
	}

	// And the note saying which version is waiting, which is what keeps the card
	// from offering the same update again before the restart.
	pending, err := os.ReadFile(self + ".pending")
	if err != nil || strings.TrimSpace(string(pending)) != version {
		t.Errorf("the pending version reads %q (%v), want %s", pending, err, version)
	}
}

// A failed install leaves the old binary where it was.
//
// The download is checked before anything is moved, so a release that does not
// match its own checksum costs nothing. An installation left with neither the old
// binary nor a working new one is the failure this ordering exists to prevent.
func TestAFailedInstallLeavesTheOldBinaryInPlace(t *testing.T) {
	const version = "v9.9.9"

	// A checksum for something else entirely.
	wrong := sha256.Sum256([]byte("not what is served"))

	source, release, _ := stagedRelease(t, version, hex.EncodeToString(wrong[:]),
		workingProgram(t, "the new version"))

	self := filepath.Join(t.TempDir(), "go-time-recording"+exeSuffix())
	old := []byte("the old version")

	if err := os.WriteFile(self, old, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := source.InstallOver(context.Background(), release, self); err == nil {
		t.Fatal("an install whose download does not match its checksum reported success")
	}

	got, err := os.ReadFile(self)
	if err != nil {
		t.Fatalf("the old binary is gone after a failed install: %v", err)
	}

	if string(got) != string(old) {
		t.Errorf("the old binary was replaced anyway, and now holds %q", got)
	}

	if _, err := os.Stat(self + ".new"); err == nil {
		t.Error("the refused download is still lying beside the binary")
	}
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}

	return ""
}

// workingProgram is a file that runs and answers --version, which is what the
// install now requires of anything it is about to make the application.
//
// A real program rather than a string of bytes: the check runs it, so a test
// that handed it text would be testing the check's error path and calling it the
// success one.
func workingProgram(t *testing.T, marker string) []byte {
	t.Helper()

	dir := t.TempDir()
	source := filepath.Join(dir, "main.go")

	program := `package main

import "fmt"

func main() { fmt.Println("MARKER") }
`

	program = strings.Replace(program, "MARKER", marker, 1)

	if err := os.WriteFile(source, []byte(program), 0o600); err != nil {
		t.Fatal(err)
	}

	built := filepath.Join(dir, "built"+exeSuffix())

	build := exec.Command("go", "build", "-o", built, source)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the stand-in: %v; %s", err, out)
	}

	body, err := os.ReadFile(built)
	if err != nil {
		t.Fatal(err)
	}

	return body
}

// A download that cannot run here is not installed.
//
// The checksum proves the bytes are the ones the release published. It says
// nothing about whether they run on this machine - a build for the wrong libc,
// an architecture that looked right, a release that is simply broken. Every one
// of those used to replace the binary and take the application down on the
// restart, at which point the screen that could have put the old one back was
// gone too.
func TestADownloadThatCannotRunIsNotInstalled(t *testing.T) {
	const version = "v9.9.9"

	broken := []byte("this is not a program")
	sum := sha256.Sum256(broken)

	source, release, _ := stagedRelease(t, version, hex.EncodeToString(sum[:]), broken)

	self := filepath.Join(t.TempDir(), "go-time-recording"+exeSuffix())
	old := workingProgram(t, "the old version")

	if err := os.WriteFile(self, old, 0o755); err != nil {
		t.Fatal(err)
	}

	err := source.InstallOver(context.Background(), release, self)
	if err == nil {
		t.Fatal("a download that cannot run was installed")
	}

	if !strings.Contains(err.Error(), "not installed") {
		t.Errorf("the refusal reads %q, which does not say the update did not happen", err)
	}

	got, err := os.ReadFile(self)
	if err != nil {
		t.Fatalf("the old binary is gone: %v", err)
	}

	if string(got) != string(old) {
		t.Error("the old binary was replaced by one that cannot run")
	}
}

// And when everything passed and the new version still will not serve, the old
// one goes back.
//
// On that path the application is not running to offer a button, so this is what
// somebody reaches for from a shell - and it has to work without the application.
func TestARollbackPutsThePreviousVersionBack(t *testing.T) {
	const version = "v9.9.9"

	newBinary := workingProgram(t, "the new version")
	sum := sha256.Sum256(newBinary)

	source, release, _ := stagedRelease(t, version, hex.EncodeToString(sum[:]), newBinary)

	self := filepath.Join(t.TempDir(), "go-time-recording"+exeSuffix())
	old := workingProgram(t, "the old version")

	if err := os.WriteFile(self, old, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := source.InstallOver(context.Background(), release, self); err != nil {
		t.Fatalf("installing: %v", err)
	}

	if err := RollbackOver(self); err != nil {
		t.Fatalf("rolling back: %v", err)
	}

	got, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != string(old) {
		t.Error("the previous version is not back at its own path")
	}

	// The one rolled back from is kept rather than dropped: whoever is doing this
	// may want to look at what they rolled back from, and it is one file.
	if _, err := os.Stat(self + ".failed"); err != nil {
		t.Error("the version that was rolled back from was thrown away")
	}

	// And the note saying an update is waiting goes, or the card would keep
	// promising a version that is no longer on disk.
	if _, err := os.Stat(self + ".pending"); err == nil {
		t.Error("the pending note survived the rollback")
	}
}

// With nothing to go back to, a rollback says so rather than removing the
// working binary.
func TestARollbackWithNothingToGoBackToIsRefused(t *testing.T) {
	self := filepath.Join(t.TempDir(), "go-time-recording"+exeSuffix())

	if err := os.WriteFile(self, []byte("the only version"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := RollbackOver(self); err == nil {
		t.Fatal("a rollback with no previous version reported success")
	}

	if _, err := os.Stat(self); err != nil {
		t.Error("the only binary was moved aside by a rollback that had nowhere to go")
	}
}

// Starting the new version clears the note, and keeps the way back.
//
// The tempting version of this clears both: the update is done, the process is
// the new version, tidy up. It is wrong, and quietly - starting is not serving.
// A version that comes up far enough to reach the start-up tidy and then fails
// on the migration, on the port, or on a certificate has already destroyed the
// binary somebody would have gone back to, in the same second it became the
// thing they needed.
func TestStartingTheNewVersionKeepsThePreviousOne(t *testing.T) {
	self := filepath.Join(t.TempDir(), "go-time-recording"+exeSuffix())

	for name, body := range map[string]string{
		"":         "the new version",
		".old":     "the version before it",
		".pending": "v9.9.9",
	} {
		if err := os.WriteFile(self+name, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	removeLeftovers(self)

	if _, err := os.Stat(self + ".pending"); err == nil {
		t.Error("the update is still reported as waiting after the version it " +
			"installed has started")
	}

	if _, err := os.Stat(self + ".old"); err != nil {
		t.Error("starting the new version threw away the one to go back to")
	}
}

// The swap keeps the version that was working, and puts it back if the install
// does not finish.
//
// Nothing covered `swap` at all, which is the function that renames the binary
// this process is running out of. Everything after it - Rollback, the "restart to
// use it" notice, the operations manual's way back from an update that installs
// and will not serve - is built on the two files it leaves behind, and none of
// that was being proved.
//
// The failure is provoked with a staged path that is not there, because that is
// the shape of every real way the second rename loses: a full disk, a different
// filesystem, an antivirus holding the download open. What matters is not which
// of those it was but that the binary at the installation's own path is the one
// that was working a moment ago, rather than nothing at all.
func TestTheSwapPutsTheWorkingBinaryBackIfTheInstallFails(t *testing.T) {
	dir := t.TempDir()
	self := filepath.Join(dir, "go-time-recording"+exeSuffix())

	if err := os.WriteFile(self, []byte("the version that works"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A leftover from an earlier update. The swap removes it first, so this also
	// pins that the copy kept is the one that was running rather than an older one.
	if err := os.WriteFile(self+".old", []byte("two versions ago"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := swap(self, filepath.Join(dir, "never-downloaded"), "v9.9.9")
	if err == nil {
		t.Fatal("an install from a staged file that is not there reported success")
	}

	if !strings.Contains(err.Error(), "cannot put the new binary in place") {
		t.Errorf("the refusal does not say which half failed: %v", err)
	}

	running, readErr := os.ReadFile(self)
	if readErr != nil {
		t.Fatalf("the installation has no binary at its own path: %v", readErr)
	}

	if string(running) != "the version that works" {
		t.Errorf("the path holds %q, want the version that was running", running)
	}

	if _, err := os.Stat(self + ".pending"); err == nil {
		t.Error("an install that failed still reports a version waiting to be run")
	}
}

// The swap that succeeds keeps the previous binary and says which version is
// waiting.
//
// The two files are the contract the rest of the update reads: RollbackOver
// refuses without the .old, and the card says "restart to use it" from the
// .pending. A swap that installed correctly and wrote neither would look exactly
// like a swap that had not run.
func TestTheSwapKeepsThePreviousBinaryAndNotesTheVersion(t *testing.T) {
	dir := t.TempDir()
	self := filepath.Join(dir, "go-time-recording"+exeSuffix())
	staged := filepath.Join(dir, "staged")

	if err := os.WriteFile(self, []byte("the version that works"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(staged, []byte("the new version"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := swap(self, staged, "v9.9.9"); err != nil {
		t.Fatalf("swapping in a staged binary: %v", err)
	}

	installed, err := os.ReadFile(self)
	if err != nil {
		t.Fatalf("nothing at the installation's own path: %v", err)
	}

	if string(installed) != "the new version" {
		t.Errorf("the path holds %q, want the staged version", installed)
	}

	previous, err := os.ReadFile(self + ".old")
	if err != nil {
		t.Fatalf("the way back was not kept: %v", err)
	}

	if string(previous) != "the version that works" {
		t.Errorf("the kept copy is %q, want the version that was running", previous)
	}

	pending, err := os.ReadFile(self + ".pending")
	if err != nil {
		t.Fatalf("nothing records which version is waiting: %v", err)
	}

	if string(pending) != "v9.9.9" {
		t.Errorf("the note reads %q, want v9.9.9", pending)
	}
}

// A refusal from the feed says what the feed said.
//
// "the release feed answered 403" was the whole message, and it reads as a
// permission problem this installation could do something about. It almost never
// is: sixty checks an hour are counted per address, so every instance behind one
// office connection draws from the same sixty, and running out answers 403.
//
// GitHub puts the reason in the body and the counters in the headers, and both
// were read and discarded.
func TestARefusedFeedExplainsItself(t *testing.T) {
	reset := time.Now().Add(37 * time.Minute).Unix()

	for name, tc := range map[string]struct {
		status    int
		remaining string
		body      string
		expect    []string
	}{
		"used up the hour's checks": {
			status:    http.StatusForbidden,
			remaining: "0",
			body:      `{"message":"API rate limit exceeded for 203.0.113.7."}`,
			expect:    []string{"per hour", "used them up"},
		},
		"refused for another reason": {
			status:    http.StatusForbidden,
			remaining: "58",
			body:      `{"message":"Repository access blocked"}`,
			expect:    []string{"403", "Repository access blocked"},
		},
		"nothing to say": {
			status:    http.StatusInternalServerError,
			remaining: "58",
			body:      ``,
			expect:    []string{"500"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			feed := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					// Identified, which the feed asks of every caller and refuses
					// some that do not.
					if agent := r.Header.Get("User-Agent"); !strings.Contains(agent, "time-recording") {
						t.Errorf("the check identifies itself as %q", agent)
					}

					w.Header().Set("X-RateLimit-Remaining", tc.remaining)
					w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset, 10))
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte(tc.body))
				}))

			t.Cleanup(feed.Close)

			_, err := New(feed.URL, "").Latest(context.Background())
			if err == nil {
				t.Fatal("a refused feed was reported as a release")
			}

			for _, want := range tc.expect {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the explanation %q does not mention %q", err, want)
				}
			}
		})
	}
}

// A token is sent where the installation has one, and nothing is sent where it
// has not - which is every installation by default.
func TestATokenIsSentOnlyWhenThereIsOne(t *testing.T) {
	for name, token := range map[string]string{"with a token": "secret-token", "without one": ""} {
		t.Run(name, func(t *testing.T) {
			var seen string

			feed := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					seen = r.Header.Get("Authorization")
					w.WriteHeader(http.StatusInternalServerError)
				}))

			t.Cleanup(feed.Close)

			_, _ = New(feed.URL, token).Latest(context.Background())

			want := ""
			if token != "" {
				want = "Bearer " + token
			}

			if seen != want {
				t.Errorf("the feed was sent %q, want %q", seen, want)
			}
		})
	}
}

// Two installs at once: one proceeds, the other is turned away.
//
// This downloads to a fixed path beside the binary and renames it over the
// binary. Two of them at once write the same staged file and rename it twice,
// and what that corrupts is the program itself - the worst thing here to get
// wrong. Two administrators pressing the button within a moment of each other,
// or one pressing it twice, is the ordinary way in.
//
// The race detector cannot find this on its own: it reports races that happen,
// and no suite installs twice at the same moment unless one is written to.
func TestOnlyOneInstallRunsAtATime(t *testing.T) {
	// A release whose download blocks until this test lets it go, so the second
	// attempt lands while the first is still inside.
	holding := make(chan struct{})
	served := make(chan struct{}, 2)

	// A real program: the install runs what it downloaded before putting it in
	// place, so a string of bytes would be testing the check's failure path and
	// calling it success.
	binary := workingProgram(t, "the new version")
	sum := sha256.Sum256(binary)

	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "SHA256SUMS"):
			_, _ = fmt.Fprintf(w, "%x  %s\n", sum, assetName("v9.9.9"))

		default:
			served <- struct{}{}
			<-holding
			_, _ = w.Write(binary)
		}
	}))

	defer feed.Close()

	source := &Source{Client: feed.Client()}

	release := Release{
		Version: "v9.9.9",
		asset:   feed.URL + "/" + assetName("v9.9.9"),
		sums:    feed.URL + "/SHA256SUMS",
	}

	self := filepath.Join(t.TempDir(), "program")

	if err := os.WriteFile(self, []byte("the version now running"), 0o755); err != nil {
		t.Fatalf("cannot lay down a program to replace: %v", err)
	}

	first := make(chan error, 1)

	go func() { first <- source.InstallOver(t.Context(), release, self) }()

	// Wait until the first one is inside the download, so the second is genuinely
	// concurrent rather than merely later.
	select {
	case <-served:
	case <-time.After(10 * time.Second):
		t.Fatal("the first install never reached the download")
	}

	// The second attempt, with a deadline on it.
	//
	// Turned away, it answers at once. Not turned away, it walks into the same
	// download and blocks there on the same channel this test has not released
	// yet - so without the guard this would hang rather than fail, and a case
	// that hangs on a regression reports a timeout instead of the reason.
	second := make(chan error, 1)

	go func() { second <- source.InstallOver(t.Context(), release, self) }()

	select {
	case err := <-second:
		if !errors.Is(err, ErrInstalling) {
			t.Errorf("a second install answered %v, want %v - both were writing the "+
				"same staged file over the same binary", err, ErrInstalling)
		}

	case <-time.After(5 * time.Second):
		t.Error("a second install neither ran nor was turned away: it is inside the " +
			"first one's download, writing the same staged file")
	}

	close(holding)

	if err := <-first; err != nil {
		t.Errorf("the first install failed: %v", err)
	}

	// And the one that ran did its work: the program on disk is the new one.
	if got, err := os.ReadFile(self); err != nil {
		t.Fatalf("reading the program back: %v", err)
	} else if string(got) != string(binary) {
		t.Error("the program on disk is not the one that was installed")
	}
}
