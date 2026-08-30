//go:build integration

package integration

import (
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The administration surface: the build version the footer shows, and the
// process log. Both are new, and both are the kind of thing that looks fine in
// a screenshot while being wrong in a way only a request would reveal - a
// version that is always "dev", a log endpoint any user can read.

// ------------------------------------------------------------------ version

// The footer asks which build is running, and the answer has to come from the
// build rather than from a constant somebody forgets to change.
func TestTheBrandingResponseCarriesTheVersion(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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

// logsContaining waits for a search to find something, and answers with what it
// found.
//
// The request that produced a line has returned by the time a case looks for it,
// which is not the same as the line having been written: it travels from the
// framework through a pipe into the sink that keeps it, and none of that is on
// the request's own path. Asking once and failing is asking whether the machine
// was quick.
//
// It cost a red pipeline on an unrelated change: "searching for a path that was
// definitely requested found nothing", 0.56 seconds into a case whose request had
// certainly been served.
func (c *client) logsContaining(t *testing.T, query string) logPage {
	t.Helper()

	deadline := time.Now().Add(20 * time.Second)

	for {
		page := c.logs(t, query)
		if len(page.Records) > 0 {
			return page
		}

		if time.Now().After(deadline) {
			return page
		}

		time.Sleep(100 * time.Millisecond)
	}
}

// The log carries email addresses, request paths and whatever a failing driver
// decided to print. A user reading it would be reading the whole
// installation's traffic.
func TestTheLogIsOnlyReadableByTheBuiltInAdministrator(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	admin.must(admin.api(http.MethodPost, "/users", map[string]any{
		"name": "Nosy", "email": "nosy@example.com",
		"role": "user", "password": "nosy-password-1",
	}), http.StatusCreated, http.StatusOK)

	user := a.newClient()
	user.signIn("nosy@example.com", "nosy-password-1")

	if got := user.api(http.MethodGet, "/admin/logs", nil).Status; got != http.StatusForbidden {
		t.Errorf("a user reading the log got %d, want 403", got)
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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	page := admin.logsContaining(t, "?search=/api/v1/projects&limit=500")

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
	t.Parallel()

	// LOG_LEVEL=INFO because the request log is an INFO line, and the harness
	// otherwise runs at WARN. Worth knowing beyond this test: the viewer can only
	// ever show what LOG_LEVEL admits, so ticking DEBUG on an installation
	// running at WARN shows nothing and is not a bug.
	a := start(t, "LOG_LEVEL=INFO")
	c := a.signInAsAdmin("a-much-better-password")

	// Waited for, for the reason logsContaining exists: signing in produced the
	// lines this reads, and a line written is not a line arrived.
	first := c.logsContaining(t, "?limit=500")
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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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

// The description of a shipped role is refused like its name and its rights.
//
// It was the one part still open, on the reasoning that a description is only
// words - but these three are the words the interface translates, keyed on the
// name. An installation that edited one got a description in one language that
// the interface then overrode in another, which reads as the change not having
// been saved.
func TestAShippedRolesDescriptionCannotBeChanged(t *testing.T) {
	t.Parallel()

	a := start(t)
	c := a.signInAsAdmin("a-much-better-password")

	var roles struct {
		Items []struct {
			ID          uint   `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"items"`
	}

	c.must(c.api(http.MethodGet, "/roles", nil), http.StatusOK).Data(t, &roles)

	var shipped struct {
		ID          uint
		Name        string
		Description string
	}

	for _, role := range roles.Items {
		if role.Name == "user" {
			shipped.ID, shipped.Name, shipped.Description = role.ID, role.Name, role.Description
		}
	}

	if shipped.ID == 0 {
		t.Fatal("the shipped user role is missing, so there is nothing to protect")
	}

	res := c.api(http.MethodPut, fmt.Sprintf("/roles/%d", shipped.ID), map[string]any{
		"description": "Something an installation typed instead",
	})

	if res.Status != http.StatusConflict {
		t.Fatalf("changing a shipped role's description answered %d, want %d",
			res.Status, http.StatusConflict)
	}

	// And it really is unchanged, rather than refused after the fact.
	var after struct {
		Items []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"items"`
	}

	c.must(c.api(http.MethodGet, "/roles", nil), http.StatusOK).Data(t, &after)

	for _, role := range after.Items {
		if role.Name == shipped.Name && role.Description != shipped.Description {
			t.Errorf("the description is now %q, was %q", role.Description, shipped.Description)
		}
	}

	// The same request against a role of the installation's own is allowed: this
	// is a rule about the three that ship, not about descriptions.
	made := c.must(c.api(http.MethodPost, "/roles", map[string]any{
		"name":        "reviewer",
		"description": "Reads what others booked",
		"permissions": []string{"timesheets:read:own"},
	}), http.StatusCreated)

	var own struct {
		ID uint `json:"id"`
	}

	made.Data(t, &own)

	c.must(c.api(http.MethodPut, fmt.Sprintf("/roles/%d", own.ID), map[string]any{
		"description": "Reads what they booked themselves",
	}), http.StatusOK)
}

// An installation configured through the environment can see what it is
// connected to.
//
// The screen is filled from the file the installer or the screen itself writes,
// and a deployment that sets DB_* has no such file - so every field was blank,
// under a first line saying "currently connected via postgres". It read as "not
// configured" on an installation that plainly was.
//
// Worse than looking wrong: the file wins over the environment. Filling in that
// form would override the deployment's own settings at the next start, and
// nothing said so.
//
// The harness starts its instances exactly that way, which is why this can be
// asked here at all.
func TestTheConnectionScreenSaysWhatIsRunningWhenNothingIsStored(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	var ds struct {
		Active  string `json:"active"`
		Stored  bool   `json:"stored"`
		Dialect string `json:"dialect"`
		Running struct {
			Dialect string `json:"dialect"`
			Name    string `json:"name"`
		} `json:"running"`
	}

	admin.must(admin.api(http.MethodGet, "/settings/datasource", nil),
		http.StatusOK).Data(t, &ds)

	if ds.Stored {
		t.Fatal("this instance is configured through the environment, so nothing " +
			"should be stored - the case is asking about the wrong state")
	}

	if ds.Running.Dialect == "" {
		t.Error("the screen is told nothing about the connection this process " +
			"opened, so it has an empty form to show and no way to say why")
	}

	if ds.Running.Dialect != ds.Active {
		t.Errorf("the running connection says %q and the active dialect says %q; "+
			"they describe the same connection", ds.Running.Dialect, ds.Active)
	}

	if ds.Running.Name == "" {
		t.Error("the running connection carries no database name, so the screen " +
			"cannot show which database it is connected to")
	}

	// And the stored fields stay empty, because nothing is stored: what is shown
	// is a placeholder, not something this form would save.
	if ds.Dialect != "" {
		t.Errorf("the stored dialect is %q on an installation that has stored "+
			"nothing; the running connection must not be presented as saved",
			ds.Dialect)
	}
}

// An administrator can let somebody back in without destroying their work.
//
// There was no way to. A password is set at creation and changed by its owner
// through /me/password, which asks for the current one - so an account whose
// owner had forgotten it could only be deleted and made again, and deleting it
// takes every hour recorded in it. The choice was somebody's time or somebody's
// access.
//
// The password is chosen by whoever resets it rather than being the documented
// one. The must-change flag stops the account using the application, but it
// deliberately does not stop signing in or setting a new password - it cannot,
// or nobody could ever get out of it - so a reset to a password printed in the
// README would leave a window in which anybody who knows the address can take
// the account over.
func TestAnAdministratorResetsAForgottenPassword(t *testing.T) {
	t.Parallel()

	app := start(t)
	admin := app.signInAsAdmin("a-much-better-password")
	worker := app.signInAsUser(admin, "Wera", "wera@example.com")

	var me struct {
		User userResponse `json:"user"`
	}

	worker.must(worker.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &me)

	// Something recorded, so a deletion would not have been an answer.
	worker.must(worker.api(http.MethodPost, "/timesheets", map[string]any{
		"date": "2026-08-03", "durationHours": 4,
	}), http.StatusCreated, http.StatusOK)

	const chosen = "a-chosen-reset-password"

	admin.must(admin.api(http.MethodPut,
		fmt.Sprintf("/users/%d/password", me.User.ID),
		map[string]any{"password": chosen}), http.StatusOK)

	// The session that was open is gone. Whoever reset this did it because
	// something was wrong, and a session still running somewhere is the thing
	// that would be left of it.
	if got := worker.api(http.MethodGet, "/me", nil).Status; got == http.StatusOK {
		t.Error("the account's open session survived a password reset")
	}

	// The new password works, and the account is made to replace it before it can
	// do anything - so what the administrator typed is not left standing as a
	// password somebody else once knew.
	returning := app.newClient()
	returning.signIn("wera@example.com", chosen)

	if got := returning.api(http.MethodGet, "/timesheets", nil).Status; got != http.StatusConflict {
		t.Errorf("the reset account reads its entries with status %d; it must be made "+
			"to choose its own password first", got)
	}

	returning.must(returning.api(http.MethodPut, "/me/password", map[string]any{
		"currentPassword": chosen, "newPassword": "wera-own-password-1",
	}), http.StatusOK)

	// And the hours are still there, which is the whole point of not deleting the
	// account.
	var entries struct {
		Items []struct {
			DurationHours float64 `json:"durationHours"`
		} `json:"items"`
	}

	returning.must(returning.api(http.MethodGet, "/timesheets", nil),
		http.StatusOK).Data(t, &entries)

	if len(entries.Items) != 1 {
		t.Errorf("the account came back with %d entries, want 1", len(entries.Items))
	}
}

// Three accounts it is refused for, and each refusal is the server's rather
// than the screen's.
func TestAPasswordResetIsRefusedWhereItWouldBeALie(t *testing.T) {
	t.Parallel()

	app := start(t)
	admin := app.signInAsAdmin("a-much-better-password")

	var me struct {
		User userResponse `json:"user"`
	}

	admin.must(admin.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &me)

	// Their own. /me/password is the way to that and it asks for the current
	// password; a route that did not would be a way around it for anybody who
	// walked up to an unlocked screen.
	if got := admin.api(http.MethodPut, fmt.Sprintf("/users/%d/password", me.User.ID),
		map[string]any{"password": "straight-past-the-old-one"}).Status; got == http.StatusOK {
		t.Error("an account reset its own password without being asked for the current one")
	}

	// Nobody who may not administer accounts.
	worker := app.signInAsUser(admin, "Wera", "wera@example.com")

	var hers struct {
		User userResponse `json:"user"`
	}

	worker.must(worker.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &hers)

	if got := worker.api(http.MethodPut, fmt.Sprintf("/users/%d/password", me.User.ID),
		map[string]any{"password": "not-mine-to-set"}).Status; got != http.StatusForbidden {
		t.Errorf("an ordinary account reset somebody else's password: status %d", got)
	}

	// And a password the account could not have used anyway.
	if got := admin.api(http.MethodPut, fmt.Sprintf("/users/%d/password", hers.User.ID),
		map[string]any{"password": "short"}).Status; got != http.StatusBadRequest {
		t.Errorf("a password below the minimum length was accepted: status %d", got)
	}
}
