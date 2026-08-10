//go:build integration

// Package integration exercises the application the way a browser and a script
// do: over HTTP, against a real database, through the real binary.
//
// The unit tests already cover the rules in isolation. What they cannot cover
// is everything between a request arriving and a rule being reached - the
// middleware order, the CSRF check, the session cookie, the migrations, the
// embedded assets, the wiring in main.go. Every bug found in this project by
// running it rather than testing it lived in exactly that gap: a login screen
// that could not be dismissed, a booking dated in UTC, a directory that could
// take over the administrator account.
//
// So this starts the compiled binary as a subprocess and talks to it. Nothing
// is stubbed.
//
//	go test -tags integration ./test/integration
//	GTR_TEST_DSN=postgres://... go test -tags integration ./test/integration
//	GTR_TEST_DSN=mysql://...    go test -tags integration ./test/integration
//
// The account in that DSN has to be allowed to CREATE DATABASE, because each
// test gets its own. On PostgreSQL the owner already can; on MySQL an ordinary
// user cannot, so use root there - it is a throwaway server either way.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	neturl "net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/test/harness"
)

const (
	adminEmail    = harness.AdminEmail
	adminPassword = harness.AdminPassword
)

func TestMain(m *testing.M) {
	cleanup, err := harness.Build()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	code := m.Run()

	cleanup()
	os.Exit(code)
}

// app is a running instance plus the helpers these tests hang off it.
type app struct {
	t *testing.T
	*harness.App
}

func start(t *testing.T, env ...string) *app {
	t.Helper()

	return &app{t: t, App: harness.Start(t, env...)}
}

func (a *app) log() string { return a.Log() }

// ------------------------------------------------------------------ client

// client is one browser session: its own cookie jar, and the CSRF handling a
// browser's script would do.
type client struct {
	t    *testing.T
	app  *app
	http *http.Client
}

// newClient opens a session and fetches the page once, which is what hands out
// the CSRF cookie.
func (a *app) newClient() *client {
	a.t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		a.t.Fatalf("cannot create a cookie jar: %v", err)
	}

	c := &client{
		t:    a.t,
		app:  a,
		http: &http.Client{Jar: jar, Timeout: 15 * time.Second},
	}

	// The visit a browser makes before anything else.
	c.do(http.MethodGet, "/", nil)

	return c
}

// response is a decoded API answer.
type response struct {
	Status int
	Body   []byte
}

// Data unmarshals GoFr's {data: …} envelope into target.
func (r response) Data(t *testing.T, target any) {
	t.Helper()

	var envelope struct {
		Data json.RawMessage `json:"data"`
	}

	if err := json.Unmarshal(r.Body, &envelope); err != nil {
		t.Fatalf("response is not JSON: %v\n%s", err, r.Body)
	}

	if err := json.Unmarshal(envelope.Data, target); err != nil {
		t.Fatalf("cannot decode data: %v\n%s", err, envelope.Data)
	}
}

// Message pulls the error text out, for asserting on a refusal.
func (r response) Message() string {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	_ = json.Unmarshal(r.Body, &envelope)

	return envelope.Error.Message
}

// csrfToken reads the cookie our own script would read.
func (c *client) csrfToken() string {
	parsed := c.app.BaseURL()

	u, err := parseURL(parsed)
	if err != nil {
		c.t.Fatalf("bad base URL: %v", err)
	}

	for _, cookie := range c.http.Jar.Cookies(u) {
		if cookie.Name == "gtr_csrf" {
			return cookie.Value
		}
	}

	return ""
}

// do performs a request the way the interface does: JSON, same-origin, and the
// CSRF token echoed on anything that changes state.
func (c *client) do(method, path string, body any) response {
	c.t.Helper()

	var reader io.Reader

	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			c.t.Fatalf("cannot encode the request body: %v", err)
		}

		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(context.Background(), method, c.app.BaseURL()+path, reader)
	if err != nil {
		c.t.Fatalf("cannot build the request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", c.app.BaseURL())

	if method != http.MethodGet && method != http.MethodHead {
		req.Header.Set("X-CSRF-Token", c.csrfToken())
	}

	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s failed: %v\n%s", method, path, err, c.app.log())
	}

	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatalf("cannot read the response: %v", err)
	}

	return response{Status: resp.StatusCode, Body: data}
}

// api is do() with the /api/v1 prefix, which is every call but the assets.
func (c *client) api(method, path string, body any) response {
	c.t.Helper()

	return c.do(method, "/api/v1"+path, body)
}

// upload posts a file as multipart/form-data, which is how the spreadsheet import
// arrives.
//
// Its own method rather than a flag on do(): the body is built differently, the
// content type carries a boundary, and putting both shapes in one function would
// mean a JSON caller could accidentally send a boundary.
func (c *client) upload(path, field, filename string, content []byte, fields map[string]string) response {
	c.t.Helper()

	var body bytes.Buffer

	form := multipart.NewWriter(&body)

	part, err := form.CreateFormFile(field, filename)
	if err != nil {
		c.t.Fatalf("cannot build the upload: %v", err)
	}

	if _, err := part.Write(content); err != nil {
		c.t.Fatalf("cannot write the upload: %v", err)
	}

	for name, value := range fields {
		if err := form.WriteField(name, value); err != nil {
			c.t.Fatalf("cannot write the field %q: %v", name, err)
		}
	}

	if err := form.Close(); err != nil {
		c.t.Fatalf("cannot close the upload: %v", err)
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		c.app.BaseURL()+"/api/v1"+path, &body)
	if err != nil {
		c.t.Fatalf("cannot build the request: %v", err)
	}

	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("Origin", c.app.BaseURL())
	req.Header.Set("X-CSRF-Token", c.csrfToken())

	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("uploading to %s failed: %v\n%s", path, err, c.app.log())
	}

	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatalf("cannot read the response: %v", err)
	}

	return response{Status: resp.StatusCode, Body: data}
}

// must fails the test unless the call returned one of the expected statuses.
func (c *client) must(r response, expected ...int) response {
	c.t.Helper()

	for _, want := range expected {
		if r.Status == want {
			return r
		}
	}

	c.t.Fatalf("expected status %v, got %d: %s", expected, r.Status, r.Body)

	return r
}

// signIn authenticates and returns the signed-in user.
func (c *client) signIn(email, password string) userResponse {
	c.t.Helper()

	r := c.must(c.api(http.MethodPost, "/auth/login",
		map[string]string{"email": email, "password": password}), http.StatusCreated, http.StatusOK)

	var result struct {
		User userResponse `json:"user"`
	}

	r.Data(c.t, &result)

	return result.User
}

// signInAsAdmin signs in and gets the initial-password requirement out of the
// way, which the server otherwise refuses everything else over.
func (a *app) signInAsAdmin(newPassword string) *client {
	a.t.Helper()

	c := a.newClient()
	c.signIn(adminEmail, adminPassword)

	c.must(c.api(http.MethodPut, "/me/password", map[string]string{
		"currentPassword": adminPassword,
		"newPassword":     newPassword,
	}), http.StatusOK)

	// The password change ends every session of that user, so this one is over.
	fresh := a.newClient()
	fresh.signIn(adminEmail, newPassword)

	return fresh
}

// signInAsUser creates an ordinary account and signs in as it.
//
// There is no second role any more. Everyone keeps their own time, projects and
// calendar, and the built-in administrator runs the installation - so a test that
// needs somebody who is not the administrator needs one of these.
func (a *app) signInAsUser(admin *client, name, email string) *client {
	a.t.Helper()

	const password = "another-password-1"

	admin.must(admin.api(http.MethodPost, "/users", map[string]any{
		"name": name, "email": email, "role": "employee", "password": password,
	}), http.StatusCreated, http.StatusOK)

	user := a.newClient()
	user.signIn(email, password)

	return user
}

// signInAsAuditor creates an account that may read everybody's entries.
//
// No default role can do that any more: everyone keeps their own time and the
// administrator runs the installation. What is left is that an administrator can
// still define a role - so a test that has to observe somebody else's entries
// builds the role it needs, which also keeps role administration honest.
func (a *app) signInAsAuditor(admin *client, name, email string) *client {
	a.t.Helper()

	const password = "auditor-password-1"

	admin.must(admin.api(http.MethodPost, "/roles", map[string]any{
		"name": "auditor", "description": "reads what others recorded",
		// timesheets:read:all is what makes this role see everybody: one right for
		// one question, whether the answer is a list, an export, a project total or
		// an overtime balance. There was a second, reports:read, for the totals; it
		// belonged to the role that reviewed other people's hours and went with it.
		"permissions": []string{
			"timesheets:read:all", "timesheets:write:all", "timesheets:transfer",
			"projects:read", "projects:write", "projects:archive", "projects:delete",
			"timesheets:read:own", "timesheets:write:own", "reports:read:own",
			"settings:write:own",
		},
	}), http.StatusCreated, http.StatusOK)

	admin.must(admin.api(http.MethodPost, "/users", map[string]any{
		"name": name, "email": email, "role": "auditor", "password": password,
	}), http.StatusCreated, http.StatusOK)

	auditor := a.newClient()
	auditor.signIn(email, password)

	return auditor
}

// ------------------------------------------------------------------- types

type userResponse struct {
	ID                 uint    `json:"id"`
	Name               string  `json:"name"`
	Email              string  `json:"email"`
	Role               string  `json:"role"`
	IsSystem           bool    `json:"isSystem"`
	IsExternal         bool    `json:"isExternal"`
	MustChangePassword bool    `json:"mustChangePassword"`
	Language           string  `json:"language"`
	Timezone           string  `json:"timezone"`
	EffectiveTimezone  string  `json:"effectiveTimezone"`
	TourSeen           bool    `json:"tourSeen"`
	DailyTargetHours   float64 `json:"dailyTargetHours"`
	MaxDailyHours      float64 `json:"maxDailyHours"`
}

type timesheetResponse struct {
	ID            uint    `json:"id"`
	UserID        uint    `json:"userId"`
	ProjectID     *uint   `json:"projectId"`
	Date          string  `json:"date"`
	DurationHours float64 `json:"durationHours"`
	Description   *string `json:"description"`
}

type projectResponse struct {
	ID      uint   `json:"id"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	OwnerID *uint  `json:"ownerId"`
}

type listOf[T any] struct {
	Items      []T  `json:"items"`
	TotalCount uint `json:"totalCount"`
}

// parseURL is url.Parse, wrapped so the import stays out of the type section.
func parseURL(raw string) (*neturl.URL, error) { return neturl.Parse(raw) }

// path builds a URL path from mixed segments, so a test does not have to
// convert ids by hand at every call site.
func path(parts ...any) string {
	var b strings.Builder

	for _, part := range parts {
		fmt.Fprintf(&b, "%v", part)
	}

	return b.String()
}

// readResponse drains and closes a response into the shape the assertions use.
func readResponse(t *testing.T, resp *http.Response) response {
	t.Helper()

	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("cannot read the response: %v", err)
	}

	return response{Status: resp.StatusCode, Body: data}
}

// ------------------------------------------------------- plain HTTP helpers

// get fetches a URL as a browser would with no session at all, which is how the
// installer and the sign-in page are reached.
func get(t *testing.T, url string) string {
	t.Helper()

	body, err := fetch(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}

	return body
}

// tryGet is get for a URL that is expected to fail while something is starting
// up. It returns an empty string rather than failing the test.
func tryGet(url string) string {
	body, err := fetch(url)
	if err != nil {
		return ""
	}

	return body
}

func fetch(url string) (string, error) {
	res, err := (&http.Client{Timeout: 10 * time.Second}).Get(url)
	if err != nil {
		return "", err
	}

	defer func() { _ = res.Body.Close() }()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}

	return string(body), nil
}

// truncate keeps a failure message readable when the body is a whole HTML page.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}

	return s[:max] + "…"
}

// eventually retries a condition for a few seconds.
//
// For the handful of things this application caches deliberately - the
// maintenance state, the operational limits - where a change takes effect within
// seconds rather than instantly. Asserting immediately would be asserting against
// the cache.
func eventually(condition func() bool) bool {
	deadline := time.Now().Add(10 * time.Second)

	for time.Now().Before(deadline) {
		if condition() {
			return true
		}

		time.Sleep(200 * time.Millisecond)
	}

	return false
}
