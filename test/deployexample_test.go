package test

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// The example deployment has to install the current release.
//
// deploy/.env.example carried a pinned version, which is right for a deployment
// and wrong for an example: the number ages with every release and nothing moves
// it. It sat at v0.1.19 while the newest release was v0.1.72 - fifty-three
// behind, which is to say without the encryption of what a database dump gives
// away, without the four corrections to figures the application computes, and
// without the security fixes. Following the documented setup installed all of
// that missing, and said nothing.
//
// Found by starting the deployment to look at something else, and noticing the
// interface reporting a version from weeks earlier.
//
// So the example follows "latest" and this holds it there. The reasoning that
// argued for a pin is still true and still in the file - a container restarted
// at 3am should not come back as a different version - which is why it now says
// to pin it in your own .env. A pin written into the example is a pin nobody
// comes back to.
func TestTheDeploymentExampleFollowsTheCurrentRelease(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("locating the repository: %v", err)
	}

	path := filepath.Join(root, "deploy", ".env.example")

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	// The assignment, not the commented example beside it.
	setting := regexp.MustCompile(`(?m)^GTR_VERSION=(.*)$`).FindStringSubmatch(string(raw))
	if setting == nil {
		t.Fatal("deploy/.env.example no longer sets GTR_VERSION, so a fresh " +
			"installation gets whatever compose defaults to and this guard is blind")
	}

	if got := setting[1]; got != "latest" {
		t.Errorf("deploy/.env.example pins GTR_VERSION=%s. A version written here "+
			"ages with every release and nothing moves it: it reached fifty-three "+
			"releases behind before anybody noticed. Leave it at \"latest\" and pin "+
			"in a real .env instead", got)
	}
}
