//go:build browser

package browser

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/dennis-dko/go-time-recording/test/harness"
	"github.com/dennis-dko/go-time-recording/test/tempdir"
)

// A browser this suite starts asks no resolver about any name but localhost.
//
// Chrome looks up a dozen names of its own as it starts - its update service,
// accounts, the push service, Google's front page - each as an A, an AAAA and
// an HTTPS query, and every case starts it with a fresh profile. Measured with
// the options launch gives on 2026-10-08: accounts.google.com,
// clients2.google.com, edgedl.me.gvt1.com, update.googleapis.com, www.google.com
// and www.gstatic.com within a third of a second of the start, a few more by
// the tenth. A whole run put about a thousand such questions a minute to the
// resolver in front of the machine running it, and that resolver - a Pi-hole -
// rate-limited the machine for seconds at a time, every program on it and not
// only the suite. Nothing a case reads needs any of it: the application
// answers on localhost.
//
// Read from Chrome's own network log rather than at a resolver, because the
// log is where Chrome decides: a DNS transaction or a lookup by the system for
// any other name is a question that left the machine. The log has to show the
// page's own lookup too, or its silence about every other name proves nothing.
func TestTheBrowserAsksAboutNoNameButLocalhost(t *testing.T) {
	t.Parallel()

	app := harness.Start(t)
	netLog := filepath.Join(tempdir.New(t), "netlog.json")

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), launch("en-US",
		chromedp.Flag("log-net-log", netLog),
		chromedp.Flag("net-log-capture-mode", "Default"),
	)...)
	defer cancelAlloc()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	ctx, cancelTimeout := context.WithTimeout(ctx, interactionTimeout)
	defer cancelTimeout()

	// Asleep on purpose: what is checked is that something does not happen,
	// which can only be watched for a while. The names above were all asked
	// within 2.7 seconds of the start, so three after the page has loaded
	// cover them.
	err := chromedp.Run(ctx,
		chromedp.Navigate(app.BaseURL()),
		chromedp.WaitVisible("body", chromedp.ByQuery),
		chromedp.Sleep(3*time.Second),
	)
	if err != nil {
		t.Fatalf("load the interface: %v", err)
	}

	// Chrome finishes the log as it exits, so it is closed before it is read.
	if err := chromedp.Cancel(ctx); err != nil {
		t.Fatalf("close the browser: %v", err)
	}

	cancelAlloc()

	asked, sawTheApplication := namesLookedUpIn(t, netLog)
	if !sawTheApplication {
		t.Fatal("the network log records no lookup of localhost, where the page came from, " +
			"so its silence about every other name proves nothing")
	}

	if len(asked) > 0 {
		t.Errorf("the browser asked the network about %d name(s): %s", len(asked), strings.Join(asked, ", "))
	}
}

// netLogLookups are the events in Chrome's network log that mean a question
// went to a resolver: its own DNS client, and the system's when Chrome uses
// that instead.
var netLogLookups = []string{"DNS_TRANSACTION", "HOST_RESOLVER_SYSTEM_TASK"}

// namesLookedUpIn reads a Chrome network log and returns every name other than
// localhost that a lookup was sent for, sorted and without repeats, and whether
// the log recorded the request to resolve localhost at all.
//
// The event types are found by name in the log's own table, and a log that no
// longer names them fails rather than reads as clean: a Chrome that renamed
// them would otherwise turn this into a check that can only pass.
func namesLookedUpIn(t *testing.T, path string) ([]string, bool) {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the network log: %v", err)
	}

	var parsed struct {
		Constants struct {
			LogEventTypes map[string]int `json:"logEventTypes"`
		} `json:"constants"`
		Events []struct {
			Type   int             `json:"type"`
			Params json.RawMessage `json:"params"`
		} `json:"events"`
	}

	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("parse the network log: %v", err)
	}

	types := parsed.Constants.LogEventTypes

	request, ok := types["HOST_RESOLVER_MANAGER_REQUEST"]
	if !ok {
		t.Fatal("this Chrome's network log has no HOST_RESOLVER_MANAGER_REQUEST; the check has to learn its new name")
	}

	lookups := map[int]bool{}

	for _, name := range netLogLookups {
		id, ok := types[name]
		if !ok {
			t.Fatalf("this Chrome's network log has no %s; the check has to learn its new name", name)
		}

		lookups[id] = true
	}

	var (
		asked             []string
		sawTheApplication bool
	)

	for _, event := range parsed.Events {
		if event.Type != request && !lookups[event.Type] {
			continue
		}

		var params struct {
			Host     string `json:"host"`
			Hostname string `json:"hostname"`
		}

		// Ignored on purpose: an event without parameters names nothing.
		_ = json.Unmarshal(event.Params, &params)

		name := params.Hostname
		if name == "" {
			name = params.Host
		}

		if event.Type == request {
			sawTheApplication = sawTheApplication || strings.Contains(name, "localhost")

			continue
		}

		if name != "" && name != "localhost" && !strings.HasSuffix(name, ".localhost") {
			asked = append(asked, name)
		}
	}

	slices.Sort(asked)

	return slices.Compact(asked), sawTheApplication
}
