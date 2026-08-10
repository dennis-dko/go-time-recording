//go:build integration

package integration

import (
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The administration surface: the build version the footer shows, and the
// process log. Both are new, and both are the kind of thing that looks fine in
// a screenshot while being wrong in a way only a request would reveal - a
// version that is always "dev", a log endpoint any employee can read.

// ------------------------------------------------------------------ version

// The footer asks which build is running, and the answer has to come from the
// build rather than from a constant somebody forgets to change.
func TestTheBrandingResponseCarriesTheVersion(t *testing.T) {
	a := start(t)
	c := a.newClient()

	var instance struct {
		Title   string `json:"title"`
		Version string `json:"version"`
	}

	c.must(c.api(http.MethodGet, "/branding", nil), http.StatusOK).Data(t, &instance)

	// The harness builds without -ldflags, so "dev" is the honest answer here.
	// What matters is that the field is populated at all: an empty one means the
	// footer would render a blank corner in every deployment.
	if instance.Version == "" {
		t.Error("no version was reported, so the footer has nothing to show")
	}
}

// Readable without a session, because the footer is on the sign-in screen too.
func TestTheVersionIsReadableBeforeSigningIn(t *testing.T) {
	a := start(t)

	body := get(t, a.BaseURL()+"/api/v1/branding")
	if !strings.Contains(body, `"version"`) {
		t.Errorf("the unauthenticated branding response has no version:\n%s", truncate(body, 300))
	}
}

// ---------------------------------------------------------------- live log

type logPage struct {
	Records []struct {
		Seq     uint64    `json:"seq"`
		Time    time.Time `json:"time"`
		Level   string    `json:"level"`
		Message string    `json:"message"`
		TraceID string    `json:"traceId"`
	} `json:"records"`
	LastSeq   uint64   `json:"lastSeq"`
	Dropped   uint64   `json:"dropped"`
	Levels    []string `json:"levels"`
	Available bool     `json:"available"`
}

func (c *client) logs(t *testing.T, query string) logPage {
	t.Helper()

	var page logPage

	c.must(c.api(http.MethodGet, "/admin/logs"+query, nil), http.StatusOK).Data(t, &page)

	return page
}

// The log carries email addresses, request paths and whatever a failing driver
// decided to print. An employee reading it would be reading the whole
// installation's traffic.
func TestTheLogIsOnlyReadableByTheBuiltInAdministrator(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	admin.must(admin.api(http.MethodPost, "/users", map[string]any{
		"name": "Nosy", "email": "nosy@example.com",
		"role": "employee", "password": "nosy-password-1",
	}), http.StatusCreated, http.StatusOK)

	employee := a.newClient()
	employee.signIn("nosy@example.com", "nosy-password-1")

	if got := employee.api(http.MethodGet, "/admin/logs", nil).Status; got != http.StatusForbidden {
		t.Errorf("an employee reading the log got %d, want 403", got)
	}

	// And without any session at all.
	anonymous := a.newClient()
	if got := anonymous.api(http.MethodGet, "/admin/logs", nil).Status; got != http.StatusUnauthorized {
		t.Errorf("an anonymous request got %d, want 401", got)
	}
}

// Capture has to be installed, or the viewer is an empty box with no
// explanation. And it has to have caught the framework's own output, which is
// the whole reason it intercepts the process rather than wrapping a logger.
func TestTheLogCapturesWhatTheFrameworkWrote(t *testing.T) {
	// LOG_LEVEL=INFO because the request log is an INFO line, and the harness
	// otherwise runs at WARN. Worth knowing beyond this test: the viewer can only
	// ever show what LOG_LEVEL admits, so ticking DEBUG on an installation
	// running at WARN shows nothing and is not a bug.
	a := start(t, "LOG_LEVEL=INFO")
	c := a.signInAsAdmin("a-much-better-password")

	page := c.logs(t, "?limit=500")

	if !page.Available {
		t.Fatalf("log capture is not installed:\n%s", a.log())
	}

	if len(page.Records) == 0 {
		t.Fatalf("nothing was captured:\n%s", a.log())
	}

	if page.LastSeq == 0 {
		t.Error("lastSeq is zero, so a polling client could never ask for what is new")
	}

	// The request log comes from GoFr's middleware, not from this application.
	// Its presence is what proves the interception works.
	var sawRequest bool

	for _, record := range page.Records {
		if strings.Contains(record.Message, "/api/v1/") {
			sawRequest = true

			break
		}
	}

	if !sawRequest {
		t.Error("no request appears in the log, so the framework's own output is not being captured")
	}

	for _, record := range page.Records {
		if record.Level == "" {
			t.Errorf("a record has no level: %+v", record)
		}

		if record.Time.IsZero() {
			t.Errorf("a record has no timestamp: %+v", record)
		}
	}
}

// The levels offered by the interface come from the server, so a filter cannot
// name a level that never appears.
func TestTheLogReportsTheLevelsItCanEmit(t *testing.T) {
	a := start(t)
	c := a.signInAsAdmin("a-much-better-password")

	page := c.logs(t, "")

	for _, want := range []string{"DEBUG", "INFO", "WARN", "ERROR"} {
		if !contains(page.Levels, want) {
			t.Errorf("%s is missing from %v", want, page.Levels)
		}
	}
}

func TestFilteringTheLogByLevel(t *testing.T) {
	a := start(t)
	c := a.signInAsAdmin("a-much-better-password")

	page := c.logs(t, "?levels=warn,error&limit=500")

	for _, record := range page.Records {
		if record.Level != "WARN" && record.Level != "ERROR" {
			t.Errorf("a %s record came back from a WARN,ERROR filter: %s", record.Level, record.Message)
		}
	}

	// The instance warns about the initial administrator password on a fresh
	// database, so there is something to find.
	if len(page.Records) == 0 {
		t.Errorf("no warnings at all, which a fresh installation should have:\n%s", a.log())
	}
}

func TestSearchingTheLog(t *testing.T) {
	// LOG_LEVEL=INFO because the request log is an INFO line, and the harness
	// otherwise runs at WARN. Worth knowing beyond this test: the viewer can only
	// ever show what LOG_LEVEL admits, so ticking DEBUG on an installation
	// running at WARN shows nothing and is not a bug.
	a := start(t, "LOG_LEVEL=INFO")
	admin := a.signInAsAdmin("a-much-better-password")

	// Not startWithWorker, because this case needs LOG_LEVEL on the instance and that
	// helper starts a plain one.
	worker := a.signInAsUser(admin, "Wera", "wera@example.com")

	// A request whose path is distinctive enough to search for, made by somebody who
	// works here: the built-in administrator holds nothing about projects, so it could
	// only produce a refusal, and a refused request is a weaker thing to search for
	// than one that was actually served.
	worker.must(worker.api(http.MethodGet, "/projects", nil), http.StatusOK)

	// The log stays the administrator's to read, so the search is made from the other
	// session.
	page := admin.logs(t, "?search=/api/v1/projects&limit=500")

	if len(page.Records) == 0 {
		t.Fatal("searching for a path that was definitely requested found nothing")
	}

	for _, record := range page.Records {
		if !strings.Contains(strings.ToLower(record.Message), "/api/v1/projects") {
			t.Errorf("a record that does not match the search came back: %s", record.Message)
		}
	}
}

// A polling viewer asks for what is new. Handing it lines it already has would
// make the view repeat itself on every refresh.
func TestSinceReturnsOnlyNewLines(t *testing.T) {
	// LOG_LEVEL=INFO because the request log is an INFO line, and the harness
	// otherwise runs at WARN. Worth knowing beyond this test: the viewer can only
	// ever show what LOG_LEVEL admits, so ticking DEBUG on an installation
	// running at WARN shows nothing and is not a bug.
	a := start(t, "LOG_LEVEL=INFO")
	c := a.signInAsAdmin("a-much-better-password")

	first := c.logs(t, "?limit=500")
	if first.LastSeq == 0 {
		t.Fatal("nothing captured yet")
	}

	// Produce something new.
	c.must(c.api(http.MethodGet, "/roles", nil), http.StatusOK)

	second := c.logs(t, path("?since=", first.LastSeq, "&limit=500"))

	for _, record := range second.Records {
		if record.Seq <= first.LastSeq {
			t.Errorf("record %d was already delivered before %d", record.Seq, first.LastSeq)
		}
	}

	if second.LastSeq <= first.LastSeq {
		t.Error("the cursor did not advance even though requests were made")
	}
}

// A limit that nobody would mean has to be clamped rather than obeyed: the
// endpoint reads a mutex-guarded buffer, and a request for a million lines
// would hold it while serialising the lot.
func TestTheLogPageSizeIsBounded(t *testing.T) {
	a := start(t)
	c := a.signInAsAdmin("a-much-better-password")

	page := c.logs(t, "?limit=999999")

	if len(page.Records) > 1000 {
		t.Errorf("got %d records, want at most 1000", len(page.Records))
	}
}

// Nonsense in the query string means "no filter" rather than an error the caller
// cannot act on - the alternative is a viewer that breaks on a stale bookmark.
func TestNonsenseLogParametersAreIgnored(t *testing.T) {
	a := start(t)
	c := a.signInAsAdmin("a-much-better-password")

	for _, query := range []string{
		"?since=not-a-number",
		"?levels=NONSENSE",
		"?limit=-5",
		"?limit=abc&since=&levels=&search=",
	} {
		if got := c.api(http.MethodGet, "/admin/logs"+query, nil).Status; got != http.StatusOK {
			t.Errorf("%s: got %d, want 200", query, got)
		}
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}

	return false
}

// The branding endpoint names the platform beside the version.
//
// The same version is published for four platforms and they do not all behave
// alike - restarting from the interface works on Linux and cannot on Windows - so
// the version alone does not say what somebody is looking at. Public, like the
// version it sits next to: it is in the footer of a page anyone can reach.
func TestBrandingNamesTheVersionAndThePlatform(t *testing.T) {
	a := start(t)
	c := a.newClient()

	var instance struct {
		Version string `json:"version"`
		OS      string `json:"os"`
	}

	// No sign-in: the sign-in screen shows the footer before there is a session.
	c.must(c.api(http.MethodGet, "/branding", nil), http.StatusOK).Data(t, &instance)

	if instance.OS != runtime.GOOS {
		t.Errorf("the instance reports the platform %q, want %q", instance.OS, runtime.GOOS)
	}

	if instance.Version == "" {
		t.Error("the instance reports no version at all")
	}
}
