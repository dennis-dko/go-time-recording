package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeInstallation answers the few routes track asks, the way the real API does -
// in GoFr's envelope - and keeps what it was sent. Its stopwatch runs until it is
// stopped, and runs again once it is started.
type fakeInstallation struct {
	mu       sync.Mutex
	sent     map[string][]map[string]any
	token    string
	refuseAs string
	stopped  bool

	// language is the one the account chose in the browser; empty is none.
	language string
}

func newFakeInstallation(t *testing.T) (*fakeInstallation, *httptest.Server) {
	t.Helper()

	f := &fakeInstallation{sent: map[string][]map[string]any{}, token: "gtr_secret"}

	server := httptest.NewServer(http.HandlerFunc(f.answer))
	t.Cleanup(server.Close)

	return f, server
}

func (f *fakeInstallation) answer(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+f.token {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"not authenticated"}}`)

		return
	}

	route := r.Method + " " + strings.TrimPrefix(r.URL.Path, "/api/v1")

	var body map[string]any

	if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
		_ = json.Unmarshal(raw, &body)
	}

	f.mu.Lock()
	f.sent[route] = append(f.sent[route], body)

	if f.refuseAs == "" {
		switch route {
		case "POST /me/timer":
			f.stopped = false
		case "POST /me/timer/stop":
			f.stopped = true
		}
	}

	stopped, language := f.stopped, f.language
	f.mu.Unlock()

	if f.refuseAs != "" && r.Method != http.MethodGet {
		refusal, _ := json.Marshal(map[string]any{"error": map[string]string{"code": "x", "message": f.refuseAs}})

		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write(refusal)

		return
	}

	data := map[string]string{
		"GET /me": `{"user":{"name":"Dana Example","effectiveTimezone":"Europe/Berlin","language":"` + language + `"}}`,
		"GET /projects": `{"items":[{"id":1,"name":"Website","status":"active"},` +
			`{"id":2,"name":"Webshop","status":"active"},{"id":3,"name":"Archive","status":"archived"},` +
			`{"id":4,"name":"Consulting","status":"active"}]}`,
		"GET /me/timer":         `{"running":true,"projectId":4,"description":"review","startedAt":"2026-10-22T07:12:00Z","elapsedHours":1.5}`,
		"POST /me/timer":        `{"running":true,"projectId":4,"startedAt":"2026-10-22T07:12:00Z","elapsedHours":0}`,
		"POST /me/timer/stop":   `{"projectId":4,"date":"2026-10-22","durationHours":1.5}`,
		"POST /me/timer/switch": `{"booked":{"projectId":4,"date":"2026-10-22","durationHours":1.5}}`,
		"POST /timesheets":      `{"projectId":4,"date":"2026-10-21","durationHours":1.5}`,
		"GET /timesheets": `{"items":[{"projectId":4,"date":"2026-10-22","durationHours":2.5,"description":"review"},` +
			`{"projectId":null,"date":"2026-10-22","durationHours":0.5}],"totalCount":2}`,
	}[route]

	if route == "GET /me/timer" && stopped {
		data = `{"running":false,"elapsedHours":0}`
	}

	if data == "" {
		w.WriteHeader(http.StatusNotFound)

		return
	}

	_, _ = io.WriteString(w, `{"data":`+data+`}`)
}

// sentTo is what the installation was sent on a route.
func (f *fakeInstallation) sentTo(route string) []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.sent[route]
}

// track runs a command line against an installation, in English, and answers
// with the status, what it said and what it complained. English is asked for
// rather than left to the system, which on a German Windows is German.
func track(t *testing.T, base, token string, args ...string) (int, string, string) {
	t.Helper()

	return trackWith(t, map[string]string{"GTR_URL": base, "GTR_TOKEN": token, "GTR_LANG": "en"}, args...)
}

// trackWith runs a command line with exactly the environment given.
func trackWith(t *testing.T, env map[string]string, args ...string) (int, string, string) {
	t.Helper()

	var out, errOut bytes.Buffer

	status := Run(context.Background(), args, func(key string) string { return env[key] }, &out, &errOut)

	return status, out.String(), errOut.String()
}

// Booking by hand names the project by the start of its name, takes the hours the
// way they are written - 1:30 - and the day given, and sends the description.
func TestBookingByHandSendsWhatTheCommandLineSaid(t *testing.T) {
	f, server := newFakeInstallation(t)

	status, out, errOut := track(t, server.URL, f.token, "book", "1:30", "consult", "-m", "call", "-d", "2026-10-21")
	if status != 0 {
		t.Fatalf("book answered %d: %s", status, errOut)
	}

	sent := f.sentTo("POST /timesheets")
	if len(sent) != 1 || sent[0]["projectId"] != 4.0 || sent[0]["durationHours"] != 1.5 ||
		sent[0]["date"] != "2026-10-21" || sent[0]["description"] != "call" {
		t.Errorf("the installation was sent %v, want 1.5 hours on Consulting on 2026-10-21 with the call", sent)
	}

	if !strings.Contains(out, "Booked 1.50 h on Consulting for 2026-10-21.") {
		t.Errorf("track said %q", out)
	}
}

// A name that fits no active project, or more than one, is said back with the
// candidates, and nothing is booked.
func TestAProjectNameThatIsNotOneProjectIsSaidBack(t *testing.T) {
	f, server := newFakeInstallation(t)

	for name, want := range map[string]string{
		"Web":     `"Web" could be any of Website, Webshop`,
		"Archive": `no active project of yours is called "Archive"`,
	} {
		status, _, errOut := track(t, server.URL, f.token, "book", "1", name)
		if status != 1 || !strings.Contains(errOut, want) {
			t.Errorf("book on %q answered %d %q, want 1 saying %s", name, status, errOut, want)
		}
	}

	if sent := f.sentTo("POST /timesheets"); len(sent) != 0 {
		t.Errorf("something was booked anyway: %v", sent)
	}
}

// Bare, with its answer going anywhere but a terminal, the client says what it
// does and exits 2, as it did before it had a screen: a script that runs it so
// gets lines, not a screen waiting for keys nobody is there to press.
func TestBareTrackSaysWhatItDoesWhereThereIsNoTerminal(t *testing.T) {
	f, server := newFakeInstallation(t)

	status, out, _ := track(t, server.URL, f.token)
	if status != 2 || !strings.Contains(out, "go-time-recording-client - ") || !strings.Contains(out, "\n  status ") {
		t.Errorf("the bare client answered %d %q, want 2 and what it does", status, out)
	}
}

// What cannot be read on the command line is a 2 and is said before anything is
// asked; what the installation refuses is a 1 in the installation's own words; and
// a token it does not know says where a token comes from.
func TestWhatGoesWrongIsSaidAndToldApartByStatus(t *testing.T) {
	f, server := newFakeInstallation(t)

	for _, tc := range []struct {
		name   string
		token  string
		args   []string
		status int
		said   string
	}{
		{"hours that are not hours", f.token, []string{"book", "lots"}, 2, `"lots" is not a number of hours`},
		{"a day that is not a day", f.token, []string{"book", "1", "-d", "21.10.2026"}, 2, "is not a day"},
		{"a command it does not know", f.token, []string{"frobnicate"}, 2, `does not know "frobnicate"`},
		{"no token", "", []string{"status"}, 2, "GTR_TOKEN"},
		{"a token the installation does not know", "gtr_wrong", []string{"status"}, 1, "API tokens"},
	} {
		status, _, errOut := track(t, server.URL, tc.token, tc.args...)
		if status != tc.status || !strings.Contains(errOut, tc.said) {
			t.Errorf("%s answered %d %q, want %d saying %q", tc.name, status, errOut, tc.status, tc.said)
		}
	}

	f.refuseAs = "project \"Consulting\" is completed and no longer accepts time entries"

	status, _, errOut := track(t, server.URL, f.token, "start", "Consulting")
	if status != 1 || !strings.Contains(errOut, "no longer accepts time entries") {
		t.Errorf("a refused start answered %d %q, want 1 in the installation's words", status, errOut)
	}
}

// The status says what the stopwatch is on and since when, in the account's own
// zone, and what today adds up to.
func TestTheStatusSaysWhatIsRunningInTheAccountsZone(t *testing.T) {
	f, server := newFakeInstallation(t)

	status, out, errOut := track(t, server.URL, f.token, "status")
	if status != 0 {
		t.Fatalf("status answered %d: %s", status, errOut)
	}

	// 07:12 UTC is 09:12 in Berlin in October.
	for _, want := range []string{"Running since 09:12 on Consulting - 1:30 so far.", "Booked today: 3.00 h."} {
		if !strings.Contains(out, want) {
			t.Errorf("status said %q, want %q in it", out, want)
		}
	}
}

// The hours are read the three ways people write them, and nothing else.
func TestHoursAreReadTheWaysPeopleWriteThem(t *testing.T) {
	for raw, want := range map[string]float64{"1.5": 1.5, "1,5": 1.5, "1:30": 1.5, "0:45": 0.75, "8": 8} {
		if got, err := (language{}).parseHours(raw); err != nil || got != want {
			t.Errorf("%q read as %v (%v), want %v", raw, got, err, want)
		}
	}

	for _, raw := range []string{"", "-1", "0", "0:00", "1:60", "NaN", "Inf", "1h"} {
		if _, err := (language{}).parseHours(raw); err == nil {
			t.Errorf("%q was read as hours", raw)
		}
	}
}
