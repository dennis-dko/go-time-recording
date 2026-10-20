//go:build integration

package integration

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

type calendarFeed struct {
	Active     bool       `json:"active"`
	Prefix     string     `json:"prefix"`
	LastReadAt *time.Time `json:"lastReadAt"`
	Path       string     `json:"path"`
}

// readFeed asks for a feed the way a calendar program does: the address and
// nothing else, no session and no token.
func readFeed(a *app, path string) response {
	return a.newClient().do(http.MethodGet, path, nil)
}

// A person's calendar feed is read at the address they were handed, by anybody
// holding it and nothing else, on every database - and taken back by a new one.
func TestACalendarFeedIsReadAtItsAddressAlone(t *testing.T) {
	t.Parallel()

	a, _, worker := startWithWorker(t)

	worker.must(worker.api(http.MethodPost, "/timesheets", map[string]any{
		"date": time.Now().Format(time.DateOnly), "durationHours": 2.5, "description": "Feed, read",
	}), http.StatusCreated, http.StatusOK)

	var made calendarFeed

	worker.must(worker.api(http.MethodPost, "/me/calendar-feed", nil),
		http.StatusCreated, http.StatusOK).Data(t, &made)

	if !made.Active || !strings.HasPrefix(made.Path, "/api/v1/calendar-feed/gtrcal_") || !strings.HasSuffix(made.Path, ".ics") {
		t.Fatalf("a new feed reads %+v, want one with its address", made)
	}

	feed := readFeed(a, made.Path)

	if feed.Status != http.StatusOK || !strings.HasPrefix(feed.Header.Get("Content-Type"), "text/calendar") {
		t.Fatalf("the address answered %d as %q, want a calendar", feed.Status, feed.Header.Get("Content-Type"))
	}

	unfolded := strings.ReplaceAll(string(feed.Body), "\r\n ", "")
	if !strings.Contains(unfolded, "SUMMARY:2.50 h booked\r\n") || !strings.Contains(unfolded, `Feed\, read`) {
		t.Errorf("the feed does not have the day booked on:\n%s", unfolded)
	}

	var state calendarFeed

	worker.must(worker.api(http.MethodGet, "/me/calendar-feed", nil), http.StatusOK).Data(t, &state)

	if !state.Active || state.Prefix != made.Prefix || state.LastReadAt == nil || state.Path != "" {
		t.Errorf("after a reading the feed is described as %+v, want it read and without its address", state)
	}

	var replaced calendarFeed

	worker.must(worker.api(http.MethodPost, "/me/calendar-feed", nil),
		http.StatusCreated, http.StatusOK).Data(t, &replaced)

	if got := readFeed(a, made.Path).Status; got != http.StatusNotFound {
		t.Errorf("the address a new one replaced answered %d, want 404", got)
	}

	if got := readFeed(a, strings.TrimSuffix(replaced.Path, ".ics")).Status; got != http.StatusOK {
		t.Errorf("the new address without .ics answered %d, want the calendar", got)
	}

	worker.must(worker.api(http.MethodDelete, "/me/calendar-feed", nil), http.StatusOK, http.StatusNoContent)

	if got := readFeed(a, replaced.Path).Status; got != http.StatusNotFound {
		t.Errorf("the address of a removed feed answered %d, want 404", got)
	}
}

// An account's feed goes with it, on every database: PostgreSQL refuses the
// deletion outright if one is left pointing at the account.
func TestDeletingAnAccountTakesItsCalendarFeedWithIt(t *testing.T) {
	t.Parallel()

	a, admin, worker := startWithWorker(t)

	var me struct {
		User userResponse `json:"user"`
	}

	worker.must(worker.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &me)

	var made calendarFeed

	worker.must(worker.api(http.MethodPost, "/me/calendar-feed", nil),
		http.StatusCreated, http.StatusOK).Data(t, &made)

	admin.must(admin.api(http.MethodDelete, path("/users/", me.User.ID), nil), http.StatusOK, http.StatusNoContent)

	if left := a.Rows(t, "calendar_feeds", "user_id = ?", me.User.ID); left != 0 {
		t.Errorf("%d feed(s) outlived the account they belonged to", left)
	}

	if got := readFeed(a, made.Path).Status; got != http.StatusNotFound {
		t.Errorf("the feed of a deleted account answered %d, want 404", got)
	}
}

// Every reading of a feed is written into the request log under its address, and
// the address is the key. Concealed, the log still says a feed was read - to the
// operator on the console and to the administrator in the viewer - without being
// a way to read it.
func TestTheProcessLogDoesNotCarryAFeedsKey(t *testing.T) {
	t.Parallel()

	// LOG_LEVEL=INFO because the request log is an INFO line and the harness
	// otherwise runs at WARN, where there would be no line to carry the key - and
	// this case would pass without a reading ever being logged.
	a := start(t, "LOG_LEVEL=INFO")
	admin := a.signInAsAdmin("a-much-better-password")
	worker := a.signInAsUser(admin, "Wera", "wera@example.com")

	var made calendarFeed

	worker.must(worker.api(http.MethodPost, "/me/calendar-feed", nil),
		http.StatusCreated, http.StatusOK).Data(t, &made)

	readFeed(a, made.Path)

	key := strings.TrimSuffix(strings.TrimPrefix(made.Path, "/api/v1/calendar-feed/gtrcal_"), ".ics")

	if !a.logShows(20*time.Second, "/api/v1/calendar-feed/gtrcal_[hidden].ics") {
		t.Fatalf("the console never said a feed was read:\n%s", a.log())
	}

	if strings.Contains(a.log(), key) {
		t.Errorf("the console carries the feed's key")
	}

	page := admin.logsContaining(t, "?search=calendar-feed/gtrcal_&limit=500")
	if len(page.Records) == 0 {
		t.Fatalf("the viewer never said a feed was read")
	}

	for _, record := range page.Records {
		if strings.Contains(record.Message, key) {
			t.Errorf("the viewer shows the feed's key: %s", record.Message)
		}
	}
}
