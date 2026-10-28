package selfupdate

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Every name an installed version downloads its update by is one the release
// publishes: this version's own, go-time-recording-web_..., and the one every
// version from before the web binary had that name asks for,
// go-time-recording_....
//
// A version that is running cannot be told a new name. A release without the
// old one is a release none of those versions can take - the card offers it
// and finds no binary for the platform - and nothing but a download by hand
// would move them on. So the second name stays for as long as such a version
// may be running, and it is the web binary copied, not built again, so the two
// names cannot become two programs under one version.
func TestTheReleasePublishesEveryNameAnInstalledVersionAsksFor(t *testing.T) {
	raw, err := os.ReadFile("../../../.github/workflows/release.yml")
	if err != nil {
		t.Fatalf("reading the release workflow: %v", err)
	}

	workflow := string(raw)

	if name := assetName("v1.2.3"); !strings.HasPrefix(name, "go-time-recording-web_v1.2.3_") {
		t.Fatalf("this version asks for %s; the case below is about the web binary's name", name)
	}

	for _, stem := range []string{"go-time-recording-web", "go-time-recording"} {
		if !regexp.MustCompile(`(?m)^\s+dist/` + regexp.QuoteMeta(stem) + `_\*\s*$`).MatchString(workflow) {
			t.Errorf("the release publishes nothing called %s_..., which installed versions ask for", stem)
		}
	}

	copied := regexp.MustCompile(`(?m)^\s+cp "dist/go-time-recording-web_\$\{VERSION\}_[^"]*" "dist/go-time-recording_\$\{VERSION\}_[^"]*"\s*$`)
	if !copied.MatchString(workflow) {
		t.Error("the release does not copy the web binary to the name installed versions ask for")
	}
}
