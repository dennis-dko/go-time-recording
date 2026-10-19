//go:build integration

package integration

import (
	"bytes"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/support/spreadsheet"
)

// Finding entries by what was written about them. The listing could be narrowed
// to a project and a range of days, and nothing else: "where is the entry for that
// ticket" meant paging through somebody's history by eye.

// searched lists the caller's entries for a query string, and their descriptions.
func searched(t *testing.T, c *client, query string) (descriptions []string, total uint) {
	t.Helper()

	var list listOf[timesheetResponse]

	c.must(c.api(http.MethodGet, "/timesheets?"+query, nil), http.StatusOK).Data(t, &list)

	for _, entry := range list.Items {
		if entry.Description != nil {
			descriptions = append(descriptions, *entry.Description)
		}
	}

	return descriptions, list.TotalCount
}

func bookDescribed(t *testing.T, c *client, date, description string) {
	t.Helper()

	c.must(c.api(http.MethodPost, "/timesheets", map[string]any{
		"date": date, "durationHours": 1, "description": description,
	}), http.StatusCreated, http.StatusOK)
}

// The text is found wherever it stands in the description and whatever its case,
// and the count that pages the listing counts the same entries the page shows.
func TestTheEntriesCanBeSearchedByTheirDescription(t *testing.T) {
	t.Parallel()

	_, _, worker := startWithWorker(t)

	bookDescribed(t, worker, "2026-08-03", "Review of ticket ABC-12")
	bookDescribed(t, worker, "2026-08-04", "weekly REVIEW meeting")
	bookDescribed(t, worker, "2026-08-05", "Lunch with the customer")

	found, total := searched(t, worker, "q=review")

	if len(found) != 2 || total != 2 {
		t.Fatalf("searching for review found %v (total %d), want the two reviews", found, total)
	}

	// Narrowed by the range as well: the same search, one day.
	found, total = searched(t, worker, "q=review&from=2026-08-04&to=2026-08-04")

	if len(found) != 1 || total != 1 || found[0] != "weekly REVIEW meeting" {
		t.Errorf("searching one day found %v (total %d), want the meeting alone", found, total)
	}
}

// A percent sign and an underscore are what a person typed, not wildcards. Left as
// they are, "1%e" would find "100% coverage" and "r_v" every review - a search that
// finds more than was asked for looks like one that works, which is why it is
// worth a case of its own.
func TestASearchTakesItsWildcardsLiterally(t *testing.T) {
	t.Parallel()

	_, _, worker := startWithWorker(t)

	bookDescribed(t, worker, "2026-08-03", "100% coverage")
	bookDescribed(t, worker, "2026-08-04", "Review")
	bookDescribed(t, worker, "2026-08-05", "snake_case naming")

	for query, want := range map[string]int{
		"1%e":   0,
		"r_v":   0,
		"100%":  1,
		"e_c":   1,
		"snake": 1,
	} {
		found, total := searched(t, worker, "q="+url.QueryEscape(query))

		if len(found) != want || int(total) != want {
			t.Errorf("searching for %q found %v (total %d), want %d", query, found, total, want)
		}
	}
}

// The export follows the search as it follows every other filter, so the file
// never holds more than the screen it was started from showed.
func TestTheExportFollowsTheSearch(t *testing.T) {
	t.Parallel()

	_, _, worker := startWithWorker(t)

	bookDescribed(t, worker, "2026-08-03", "Review of ticket ABC-12")
	bookDescribed(t, worker, "2026-08-04", "Lunch with the customer")

	exported := worker.must(worker.api(http.MethodGet, "/timesheets/export?q=ticket", nil), http.StatusOK)

	rows, problems, err := spreadsheet.Read(bytes.NewReader(exported.Body))
	if err != nil || len(problems) != 0 {
		t.Fatalf("reading the export back: %v %v", err, problems)
	}

	if len(rows) != 1 || rows[0].Description != "Review of ticket ABC-12" {
		t.Errorf("the export of a search holds %+v, want the one entry found", rows)
	}
}

// A search longer than any description could be is refused by name rather than
// sent to the database.
func TestASearchLongerThanAnyDescriptionIsRefused(t *testing.T) {
	t.Parallel()

	_, _, worker := startWithWorker(t)

	refused := worker.api(http.MethodGet, "/timesheets?q="+strings.Repeat("x", 2001), nil)

	if refused.Status != http.StatusBadRequest {
		t.Errorf("an over-long search answered %d, want 400: %.200s", refused.Status, refused.Body)
	}
}
