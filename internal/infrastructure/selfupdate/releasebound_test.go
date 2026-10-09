package selfupdate

import (
	"os"
	"regexp"
	"strconv"
	"testing"
)

// The release refuses an asset that the versions already installed could not
// download, and the bound it refuses at is no larger than theirs.
//
// maxDownload is compiled into every version that is running. The day a release
// outgrows it, every installation refuses that release - the card offers it and
// the install is turned away - and none of them can be given a larger bound but
// by hand. So the release has to stop at the bound, before anything is published,
// rather than leave it to be found by everybody at once. Every version that can
// update itself has carried 100 << 20 since the package was written, so the
// workflow's number may stay below a bound raised later, and never above one.
func TestTheReleaseRefusesWhatNoInstalledVersionCanDownload(t *testing.T) {
	raw, err := os.ReadFile("../../../.github/workflows/release.yml")
	if err != nil {
		t.Fatalf("reading the release workflow: %v", err)
	}

	match := regexp.MustCompile(`(?m)^\s+largest_installable=(\d+)\s*$`).FindSubmatch(raw)
	if match == nil {
		t.Fatal("the release workflow sets no largest_installable, so nothing stops it " +
			"publishing an asset every installed version refuses to download")
	}

	limit, err := strconv.ParseInt(string(match[1]), 10, 64)
	if err != nil {
		t.Fatalf("largest_installable is not a number: %v", err)
	}

	if limit > maxDownload {
		t.Errorf("the release lets through assets up to %d bytes, and this version refuses "+
			"anything over %d", limit, maxDownload)
	}
}
