package test

import (
	"os"
	"regexp"
	"testing"
)

// The route main keeps out of the log viewer's ring is the route the viewer
// polls.
//
// The two are written in different files - the path in main, the registration in
// the router - and nothing else ties them: a route renamed in the router alone
// would put every poll back into the ring the viewer reads, and the viewer would
// go on looking at itself with no test noticing.
func TestTheQuietRouteIsTheOneTheLogViewerPolls(t *testing.T) {
	main, err := os.ReadFile("../cmd/main.go")
	if err != nil {
		t.Fatal(err)
	}

	router, err := os.ReadFile("../internal/interface/api/v1/router.go")
	if err != nil {
		t.Fatal(err)
	}

	quiet := regexp.MustCompile(`QuietRequestsTo\("([^"]+)"\)`).FindSubmatch(main)
	if quiet == nil {
		t.Fatal("main keeps no route out of the log viewer's ring")
	}

	base := regexp.MustCompile(`const base = "([^"]+)"`).FindSubmatch(router)
	route := regexp.MustCompile(`app\.GET\(base\+"([^"]+)",\s*h\.Logs\.Logs\)`).FindSubmatch(router)

	if base == nil || route == nil {
		t.Fatal("the router no longer registers the log viewer's route in the shape this reads")
	}

	if polled := string(base[1]) + string(route[1]); polled != string(quiet[1]) {
		t.Errorf("main keeps %s out of the ring, and the log viewer polls %s", quiet[1], polled)
	}
}
