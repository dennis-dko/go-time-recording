package selfupdate

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The release builds this version's binary from ./cmd and publishes it under the
// name this version downloads its update by.
//
// A running version cannot be told a new name, so a release that published the
// web binary under any other one would be offered on every installation and then
// found to have no binary for its platform. The name has been changed once, to
// go-time-recording-web, and the versions from before it ask for the old one,
// which releases no longer carry; that was decided rather than overlooked, and
// this is what keeps it from happening a second time by accident.
func TestTheReleasePublishesTheNameThisVersionUpdatesBy(t *testing.T) {
	raw, err := os.ReadFile("../../../.github/workflows/release.yml")
	if err != nil {
		t.Fatalf("reading the release workflow: %v", err)
	}

	workflow := string(raw)

	asked := assetName("v1.2.3")

	stem, _, found := strings.Cut(asked, "_v1.2.3_")
	if !found {
		t.Fatalf("this version asks for %s, which is not <name>_<version>_<os>_<arch>", asked)
	}

	if !regexp.MustCompile(`(?m)^\s+compile ` + regexp.QuoteMeta(stem) + ` \./cmd "\$@"\s*$`).MatchString(workflow) {
		t.Errorf("the release builds no %s from ./cmd, and this version asks for %s", stem, asked)
	}

	if !regexp.MustCompile(`(?m)^\s+dist/` + regexp.QuoteMeta(stem) + `_\*\s*$`).MatchString(workflow) {
		t.Errorf("the release publishes nothing called %s_..., and this version asks for %s", stem, asked)
	}
}
