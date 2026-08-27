//go:build integration

package integration

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// These are the properties that stop being true silently. A CSRF check that
// was accidentally bypassed, a permission that stopped being enforced, a token
// that outlived the role behind it - none of them change how the interface
// looks, so only a test noticing is what keeps them.

// ------------------------------------------------------------------- CSRF

func TestCSRFRejectsARequestFromAnotherSite(t *testing.T) {
	t.Parallel()

	a, _, worker := startWithWorker(t)

	// A real session and a real token - the only thing wrong is where the
	// request claims to come from, which is exactly the attack.
	req := rawRequest(t, http.MethodPost, a.BaseURL()+"/api/v1/projects",
		`{"name":"Pwned","startDate":"2026-08-02"}`)
	req.Header.Set("Origin", "http://evil.example.net")
	req.Header.Set("X-CSRF-Token", worker.csrfToken())

	resp := send(t, worker, req)
	if resp.Status != http.StatusForbidden {
		t.Fatalf("a cross-site write must be refused, got %d: %s", resp.Status, resp.Body)
	}
}

func TestCSRFRejectsAMissingToken(t *testing.T) {
	t.Parallel()

	a, _, worker := startWithWorker(t)

	req := rawRequest(t, http.MethodPost, a.BaseURL()+"/api/v1/projects",
		`{"name":"No token","startDate":"2026-08-02"}`)
	req.Header.Set("Origin", a.BaseURL())

	if resp := send(t, worker, req); resp.Status != http.StatusForbidden {
		t.Fatalf("a write without the header must be refused, got %d", resp.Status)
	}
}

func TestCSRFTokenIsRotatedOnSignIn(t *testing.T) {
	t.Parallel()

	a := start(t)
	a.signInAsAdmin("a-much-better-password")

	c := a.newClient()

	before := c.csrfToken()
	if before == "" {
		t.Fatal("a first visit must hand out a token")
	}

	c.signIn(adminEmail, "a-much-better-password")

	if after := c.csrfToken(); after == before {
		// A token planted on an anonymous visitor must not carry into their
		// session, or whoever planted it knows the value protecting it.
		t.Error("the token must be replaced on sign-in")
	}
}

func TestReadsNeedNoToken(t *testing.T) {
	t.Parallel()

	a, _, worker := startWithWorker(t)

	req := rawRequest(t, http.MethodGet, a.BaseURL()+"/api/v1/timesheets", "")

	if resp := send(t, worker, req); resp.Status != http.StatusOK {
		t.Errorf("a read must not require a CSRF token, got %d", resp.Status)
	}
}

func TestSecurityHeadersAreSet(t *testing.T) {
	t.Parallel()

	a := start(t)

	resp, err := http.Get(a.BaseURL() + "/")
	if err != nil {
		t.Fatalf("cannot fetch the page: %v", err)
	}

	defer func() { _ = resp.Body.Close() }()

	expected := map[string]string{
		"Content-Security-Policy": "default-src 'self'",
		"X-Frame-Options":         "DENY",
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "no-referrer",
	}

	// The two directives the markup is written against, named rather than left
	// inside "default-src 'self'". Widening either is what would let an inline
	// script or an inline style into the page, and a violation is silent: the
	// browser drops the offending thing and renders the rest, so the page still
	// works and only the part that needed it is quietly wrong. The markup side is
	// held by TestNoInlineScriptInTheMarkup and TestNoInlineStyleInTheMarkup.
	for _, directive := range []string{"script-src 'self'", "style-src 'self'"} {
		if got := resp.Header.Get("Content-Security-Policy"); !strings.Contains(got, directive) {
			t.Errorf("the policy no longer says %q, so the markup's assumptions "+
				"about it are no longer true: %q", directive, got)
		}
	}

	for header, want := range expected {
		if got := resp.Header.Get(header); !strings.Contains(got, want) {
			t.Errorf("%s: expected to contain %q, got %q", header, want, got)
		}
	}

	// HSTS over plain HTTP could lock a host out of working at all.
	if got := resp.Header.Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS must not be sent over plain HTTP, got %q", got)
	}
}

func TestAPIResponsesAreNotCached(t *testing.T) {
	t.Parallel()

	a, _, worker := startWithWorker(t)

	req := rawRequest(t, http.MethodGet, a.BaseURL()+"/api/v1/me", "")

	resp, err := worker.http.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	defer func() { _ = resp.Body.Close() }()

	// These carry working time and personal data; a shared cache must not keep
	// them.
	if got := resp.Header.Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Errorf("expected no-store on an API response, got %q", got)
	}
}

// -------------------------------------------------------------------- RBAC

func TestAUserSeesOnlyTheirOwnEntries(t *testing.T) {
	t.Parallel()

	a, admin, colleague := startWithWorker(t)

	// Somebody else's hours have to be recorded for "only their own" to mean
	// anything, and the administrator can no longer record them: it administers the
	// accounts and holds nothing about time. So a colleague books them, and the
	// administrator only opens the second account.
	var theirs timesheetResponse
	colleague.must(colleague.api(http.MethodPost, "/timesheets", map[string]any{
		"date": "2026-08-02", "durationHours": 4, "description": "A colleague's work",
	}), http.StatusCreated, http.StatusOK).Data(t, &theirs)

	user := a.signInAsUser(admin, "Erika", "erika@example.com")

	// Who the session belongs to, asked of the server rather than taken from the
	// entry below - otherwise the check further down compares an answer with itself.
	var me struct {
		User userResponse `json:"user"`
	}

	user.must(user.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &me)

	user.must(user.api(http.MethodPost, "/timesheets", map[string]any{
		"date": "2026-08-02", "durationHours": 3, "description": "My work",
	}), http.StatusCreated, http.StatusOK)

	var list listOf[timesheetResponse]
	user.must(user.api(http.MethodGet, "/timesheets", nil), http.StatusOK).Data(t, &list)

	if len(list.Items) != 1 {
		t.Fatalf("a user should see only their own entry, got %d", len(list.Items))
	}

	if list.Items[0].UserID != me.User.ID {
		t.Errorf("the visible entry belongs to %d, not to them (%d)",
			list.Items[0].UserID, me.User.ID)
	}

	// Asking for somebody else's is refused outright rather than filtered down
	// to nothing. The stricter answer is the better one: an empty list would be
	// indistinguishable from "that person has booked nothing", and would invite
	// building a client that quietly relies on the filter being honoured. The id
	// named is the colleague who really does have an entry, so a filter that was
	// merely honoured instead of refused would be visible as the wrong answer.
	refused := user.api(http.MethodGet, path("/timesheets?userId=", theirs.UserID), nil)
	if refused.Status != http.StatusForbidden {
		t.Errorf("a filter naming someone else must be refused, got %d", refused.Status)
	}
}

// Reading somebody else's entry by its id must not be distinguishable from
// reading one that was never there.
//
// This answered 403 for a colleague's entry and 404 for an id nobody holds,
// which is a way to ask the server which ids are real: walk the numbers and the
// status tells you where the entries are, how many there are and roughly when
// they were made, without ever being allowed to read one.
//
// The application already takes the other reading everywhere it matters - a
// private project answers not-found for exactly this reason - so this is the
// same rule applied to the entries booked against them.
func TestAnotherPersonsEntryIsIndistinguishableFromNoEntry(t *testing.T) {
	t.Parallel()

	a, admin, colleague := startWithWorker(t)

	var theirs timesheetResponse
	colleague.must(colleague.api(http.MethodPost, "/timesheets", map[string]any{
		"date": "2026-08-03", "durationHours": 4, "description": "A colleague's work",
	}), http.StatusCreated, http.StatusOK).Data(t, &theirs)

	user := a.signInAsUser(admin, "Rolf", "rolf@example.com")

	theirEntry := user.api(http.MethodGet, path("/timesheets/", theirs.ID), nil)

	// An id far past anything this test created, so the two answers being equal
	// is the point rather than a coincidence of ordering.
	noEntry := user.api(http.MethodGet, path("/timesheets/", theirs.ID+9000), nil)

	if theirEntry.Status != noEntry.Status {
		t.Errorf("an entry that exists answers %d and one that does not answers %d, "+
			"which tells the caller which ids are real",
			theirEntry.Status, noEntry.Status)
	}

	if theirEntry.Status != http.StatusNotFound {
		t.Errorf("somebody else's entry should read as not being there, got %d", theirEntry.Status)
	}
}

func TestAUserCannotAdministerUsersOrRoles(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	admin.must(admin.api(http.MethodPost, "/users", map[string]any{
		"name": "Erika", "email": "erika@example.com",
		"role": "user", "password": "erika-password-1",
	}), http.StatusCreated, http.StatusOK)

	user := a.newClient()
	user.signIn("erika@example.com", "erika-password-1")

	refused := []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/users", nil},
		{http.MethodPost, "/users", map[string]any{
			"name": "Sneaky", "email": "sneaky@example.com", "role": "admin",
		}},
		{http.MethodPost, "/roles", map[string]any{"name": "superuser"}},
		// GET /overtime was here, the team-wide balance. It is not forbidden any
		// more, it is gone: comparing colleagues is the one thing this arrangement
		// says nobody does, so the route was removed rather than guarded. That it
		// answers 404 is TestThereIsNoTeamWideOvertimeEndpoint's business, and a
		// removed route is a stronger promise than a refused one.
		{http.MethodGet, "/settings/ldap", nil},
		{http.MethodPut, "/settings/operational", map[string]any{"rateLimit": 9999}},
	}

	for _, call := range refused {
		r := user.api(call.method, call.path, call.body)
		if r.Status != http.StatusForbidden {
			t.Errorf("%s %s should be forbidden, got %d", call.method, call.path, r.Status)
		}
	}
}

func TestTheBuiltInAdministratorCannotBeDeleted(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	var list listOf[userResponse]
	admin.must(admin.api(http.MethodGet, "/users", nil), http.StatusOK).Data(t, &list)

	var systemID uint

	for _, user := range list.Items {
		if user.IsSystem {
			systemID = user.ID
		}
	}

	if systemID == 0 {
		t.Fatal("no built-in administrator found")
	}

	// It is the guaranteed way back into an installation, so removing it must
	// be refused rather than merely discouraged.
	if r := admin.api(http.MethodDelete, path("/users/", systemID), nil); r.Status == http.StatusOK {
		t.Fatal("deleting the built-in administrator must be refused")
	}

	admin.must(admin.api(http.MethodGet, path("/users/", systemID), nil), http.StatusOK)
}

// ------------------------------------------------------------- API tokens

func TestAPITokenWorksAndCarriesTheOwnersRole(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	admin.must(admin.api(http.MethodPost, "/users", map[string]any{
		"name": "Erika", "email": "erika@example.com",
		"role": "user", "password": "erika-password-1",
	}), http.StatusCreated, http.StatusOK)

	user := a.newClient()
	user.signIn("erika@example.com", "erika-password-1")

	// The value is returned as "secret", and only on creation.
	var created struct {
		Secret string `json:"secret"`
		ID     uint   `json:"id"`
	}

	user.must(user.api(http.MethodPost, "/me/tokens",
		map[string]any{"name": "ci", "expiresInDays": 0}),
		http.StatusCreated, http.StatusOK).Data(t, &created)

	if created.Secret == "" {
		t.Fatal("a token must be returned once, at creation")
	}

	// A token authenticates without a cookie and without a CSRF token: a
	// browser never sends that header by itself, so there is nothing to forge.
	req := rawRequest(t, http.MethodGet, a.BaseURL()+"/api/v1/timesheets", "")
	req.Header.Set("Authorization", "Bearer "+created.Secret)

	if resp := sendPlain(t, req); resp.Status != http.StatusOK {
		t.Fatalf("the token should authenticate, got %d: %s", resp.Status, resp.Body)
	}

	// It carries the owner's role, not more: a user's token cannot
	// administer users.
	req = rawRequest(t, http.MethodGet, a.BaseURL()+"/api/v1/users", "")
	req.Header.Set("X-API-Token", created.Secret)

	if resp := sendPlain(t, req); resp.Status != http.StatusForbidden {
		t.Errorf("the token must not outrank its owner, got %d", resp.Status)
	}

	// Revoking takes effect on the next call.
	user.must(user.api(http.MethodDelete, path("/me/tokens/", created.ID), nil),
		http.StatusOK, http.StatusNoContent)

	req = rawRequest(t, http.MethodGet, a.BaseURL()+"/api/v1/timesheets", "")
	req.Header.Set("Authorization", "Bearer "+created.Secret)

	if resp := sendPlain(t, req); resp.Status == http.StatusOK {
		t.Error("a revoked token must stop working immediately")
	}
}

func TestTokenValueIsNeverReturnedAgain(t *testing.T) {
	t.Parallel()

	_, _, worker := startWithWorker(t)

	var created struct {
		Secret string `json:"secret"`
		Prefix string `json:"prefix"`
	}

	worker.must(worker.api(http.MethodPost, "/me/tokens",
		map[string]any{"name": "once", "expiresInDays": 0}),
		http.StatusCreated, http.StatusOK).Data(t, &created)

	listed := worker.must(worker.api(http.MethodGet, "/me/tokens", nil), http.StatusOK)

	if created.Secret == "" {
		t.Fatal("nothing was created, so the check below would pass vacuously")
	}

	if strings.Contains(string(listed.Body), created.Secret) {
		t.Error("the token value must never appear again after creation")
	}
}

// ------------------------------------------------------------------ helpers

func rawRequest(t *testing.T, method, url, body string) *http.Request {
	t.Helper()

	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}

	var (
		req *http.Request
		err error
	)

	if reader != nil {
		req, err = http.NewRequestWithContext(context.Background(), method, url, reader)
	} else {
		req, err = http.NewRequestWithContext(context.Background(), method, url, nil)
	}

	if err != nil {
		t.Fatalf("cannot build the request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	return req
}

// send uses the client's jar, so the session cookie travels with it.
func send(t *testing.T, c *client, req *http.Request) response {
	t.Helper()

	resp, err := c.http.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	return readResponse(t, resp)
}

// sendPlain uses no jar at all, which is how a script calls the API.
func sendPlain(t *testing.T, req *http.Request) response {
	t.Helper()

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	return readResponse(t, resp)
}
