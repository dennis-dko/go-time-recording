package hosting

import (
	"os"
	"strings"
	"time"
)

// ZoneName names the zone this process reads its clock in, for a sentence
// somebody will read.
//
// Go names the zone itself where TZ chose it, and calls it UTC where it fell
// back to that - a TZ it cannot load included, so reading TZ here would name a
// zone the process is not in. Where the system chose, Go says "Local", on Linux
// for the zone /etc/localtime links to and on Windows for whatever the registry
// holds; the link is followed, and the registry is not named.
func ZoneName() string {
	if name := time.Local.String(); name != "Local" {
		return name
	}

	if target, err := os.Readlink("/etc/localtime"); err == nil {
		if _, name, found := strings.Cut(target, "zoneinfo/"); found {
			return name
		}
	}

	return "this machine's zone"
}
