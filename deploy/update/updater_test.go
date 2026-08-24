// Package update tests the script that holds the Docker socket.
//
// Not with Docker. What can be proven without a daemon is the half that decides
// things: what the script does when a request appears, what it writes back,
// what it refuses, and - the one that matters most - whether a failure leaves
// the running installation alone. A stub on the PATH answers as the docker
// command would, and records what it was asked.
//
// The half that cannot be proven here is whether `docker compose up -d --no-deps`
// does what the comment says it does. That is Docker's behaviour rather than
// this script's, and the place it is proven is a deployment.
package update_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stub writes a fake docker command that answers the way the script expects,
// and records every call.
//
// Two image ids so "something newer was published" and "nothing was" are
// different runs: the script compares what the service was built from before
// and after the pull, and answering the same thing twice is how it recognises
// an installation that is already current.
func stub(t *testing.T, before, after string, failOn string) (dir, calls string) {
	t.Helper()

	dir = t.TempDir()
	calls = filepath.Join(dir, "calls")

	script := `#!/bin/sh
echo "$@" >> "` + calls + `"

case "$*" in
  *"` + failOn + `"*)
    if [ -n "` + failOn + `" ]; then
      echo "pretend failure" >&2
      exit 1
    fi
    ;;
esac

case "$*" in
  "compose images --quiet "*)
    if [ -f "` + dir + `/pulled" ]; then
      echo "` + after + `"
    else
      echo "` + before + `"
    fi
    ;;
  "compose pull "*)
    touch "` + dir + `/pulled"
    ;;
  "image rm "*)
    echo "removed"
    ;;
esac

exit 0
`

	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o700); err != nil {
		t.Fatalf("cannot write the stub: %v", err)
	}

	return dir, calls
}

// run starts the script against a request directory and stops it once it has
// answered.
func run(t *testing.T, stubDir, requests string) string {
	t.Helper()

	script, err := filepath.Abs("updater.sh")
	if err != nil {
		t.Fatalf("cannot find the script: %v", err)
	}

	cmd := exec.Command("sh", script)

	cmd.Env = append(os.Environ(),
		"PATH="+stubDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GTR_UPDATE_REQUESTS="+requests,
		"GTR_UPDATE_SERVICE=app",
		// The loop sleeps between looks, and this test is waiting on it.
		"GTR_UPDATE_POLL=1",
	)

	if err := cmd.Start(); err != nil {
		t.Fatalf("cannot start the updater: %v", err)
	}

	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	// The answer, or the time it should have taken several times over.
	deadline := time.Now().Add(30 * time.Second)

	for time.Now().Before(deadline) {
		if raw, err := os.ReadFile(filepath.Join(requests, "result")); err == nil {
			return strings.TrimSpace(string(raw))
		}

		time.Sleep(100 * time.Millisecond)
	}

	t.Fatal("the updater never answered")

	return ""
}

// The ordinary run: a newer image exists, the container is recreated from it,
// and the image it replaced is removed.
func TestANewImageIsPulledTheContainerRecreatedAndTheOldImageRemoved(t *testing.T) {
	t.Parallel()

	stubDir, calls := stub(t, "sha256:old", "sha256:new", "")
	requests := t.TempDir()

	if err := os.WriteFile(filepath.Join(requests, "request"), nil, 0o600); err != nil {
		t.Fatalf("cannot leave the request: %v", err)
	}

	if got := run(t, stubDir, requests); got != "ok" {
		t.Fatalf("the update came to %q", got)
	}

	asked, err := os.ReadFile(calls)
	if err != nil {
		t.Fatalf("nothing was asked of docker: %v", err)
	}

	said := string(asked)

	for _, want := range []string{
		"compose pull app",
		"compose up -d --no-deps app",
		"image rm sha256:old",
	} {
		if !strings.Contains(said, want) {
			t.Errorf("docker was never asked to %q; it was asked:\n%s", want, said)
		}
	}

	// The one thing this must never do. An unscoped `up` would recreate the
	// database beside the application, and the updater itself in the middle of
	// the update it is performing.
	for _, forbidden := range []string{"up -d\n", "compose up -d --no-deps postgres"} {
		if strings.Contains(said, forbidden) {
			t.Errorf("docker was asked %q, which reaches past the application", forbidden)
		}
	}

	// And the request is taken, so it does not run again on the next look.
	if _, err := os.Stat(filepath.Join(requests, "request")); err == nil {
		t.Error("the request was left behind and will be acted on again")
	}
}

// Nothing newer: said, not done.
//
// A recreate that changes nothing still takes the application away from
// everybody using it, which is a poor answer to "check for updates".
func TestAnInstallationAlreadyOnTheNewestImageIsLeftAlone(t *testing.T) {
	t.Parallel()

	stubDir, calls := stub(t, "sha256:same", "sha256:same", "")
	requests := t.TempDir()

	if err := os.WriteFile(filepath.Join(requests, "request"), nil, 0o600); err != nil {
		t.Fatalf("cannot leave the request: %v", err)
	}

	if got := run(t, stubDir, requests); got != "none" {
		t.Fatalf("an installation that is already current came to %q", got)
	}

	asked, _ := os.ReadFile(calls)

	if strings.Contains(string(asked), "up -d") {
		t.Error("the container was recreated although the image had not changed")
	}
}

// A pull that fails leaves the installation running and says what happened.
func TestAFailedPullLeavesTheRunningContainerAlone(t *testing.T) {
	t.Parallel()

	stubDir, calls := stub(t, "sha256:old", "sha256:new", "compose pull")
	requests := t.TempDir()

	if err := os.WriteFile(filepath.Join(requests, "request"), nil, 0o600); err != nil {
		t.Fatalf("cannot leave the request: %v", err)
	}

	got := run(t, stubDir, requests)

	if !strings.HasPrefix(got, "failed:") {
		t.Fatalf("a failed pull came to %q", got)
	}

	if !strings.Contains(got, "pretend failure") {
		t.Errorf("the answer does not carry what went wrong: %q", got)
	}

	asked, _ := os.ReadFile(calls)

	if strings.Contains(string(asked), "up -d") {
		t.Error("the container was recreated after the pull failed, so it was " +
			"recreated from the image that was already there")
	}

	// And the marker is cleared, or nothing could ever be asked again.
	if _, err := os.Stat(filepath.Join(requests, "running")); err == nil {
		t.Error("the working marker outlived the failure and will refuse every " +
			"future request")
	}
}
