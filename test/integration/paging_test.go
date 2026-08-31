//go:build integration

package integration

import (
	"fmt"
	"net/http"
	"testing"
)

// A listing answers with a page, and says how many there are altogether.
//
// GET /timesheets had no bound of any kind: the query it ran was every row for
// the caller, ordered by date, and the entries screen asked for exactly that on
// every load and after every save. One person's own history is the blast radius
// rather than the whole table, which is what kept this from being urgent - but a
// screen that gets slower every week until somebody notices is the shape of a
// defect nobody ever files.
//
// A limit past the maximum is refused rather than quietly reduced. That is this
// application's own rule about listings, written down in the OpenAPI description
// of this very endpoint: a list that does not match what was asked for is worse
// than a plain no.

// bookDays records one entry per day and returns how many there are.
func bookDays(t *testing.T, c *client, days int) int {
	t.Helper()

	for day := 1; day <= days; day++ {
		entryOf(t, c, map[string]any{
			"date":          fmt.Sprintf("2026-08-%02d", day),
			"durationHours": 1,
		})
	}

	return days
}

// page reads one page of entries.
func page(t *testing.T, c *client, query string) listOf[timesheetResponse] {
	t.Helper()

	var out listOf[timesheetResponse]

	c.must(c.api(http.MethodGet, "/timesheets"+query, nil), http.StatusOK).Data(t, &out)

	return out
}

func TestAListingReturnsOnePageAndTheTrueTotal(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")
	anna := a.signInAsUser(admin, "Anna", "anna@example.com")

	total := bookDays(t, anna, 5)

	first := page(t, anna, "?limit=2")

	if len(first.Items) != 2 {
		t.Errorf("asked for 2 entries and got %d", len(first.Items))
	}

	if first.TotalCount != uint(total) {
		t.Errorf("totalCount is %d; a page has to say how many there are altogether, "+
			"or nobody can tell a short page from the end of the list", first.TotalCount)
	}
}

func TestTheSecondPageDoesNotRepeatTheFirst(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")
	anna := a.signInAsUser(admin, "Anna", "anna@example.com")

	bookDays(t, anna, 5)

	first := page(t, anna, "?limit=2")
	second := page(t, anna, "?limit=2&offset=2")
	last := page(t, anna, "?limit=2&offset=4")

	if len(second.Items) != 2 || len(last.Items) != 1 {
		t.Fatalf("pages of 2 over 5 entries came out as %d, %d and %d",
			len(first.Items), len(second.Items), len(last.Items))
	}

	seen := make(map[uint]bool)

	for _, p := range []listOf[timesheetResponse]{first, second, last} {
		for _, entry := range p.Items {
			if seen[entry.ID] {
				t.Errorf("entry %d appears on more than one page, so paging through "+
					"the list would show it twice and hide another", entry.ID)
			}

			seen[entry.ID] = true
		}
	}

	if len(seen) != 5 {
		t.Errorf("paging through returned %d distinct entries out of 5", len(seen))
	}
}

func TestAListingWithNoLimitIsStillBounded(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")
	anna := a.signInAsUser(admin, "Anna", "anna@example.com")

	bookDays(t, anna, 5)

	// Not a claim about the size of the default page - only that one exists, and
	// that asking for nothing in particular cannot mean "every row you have".
	all := page(t, anna, "")

	if all.PageSize == 0 {
		t.Fatal("the response does not say what page size it applied, so a client " +
			"cannot tell a complete answer from a truncated one")
	}

	if uint(len(all.Items)) > all.PageSize {
		t.Errorf("the default answer holds %d entries with a page size of %d",
			len(all.Items), all.PageSize)
	}
}

// The opening request of every paging client, and the one the screen sends.
//
// parseUint refuses a zero because it reads ids, where zero is how "not given"
// arrives - so the first page of the listing was answered with "invalid
// parameter(s): offset" while every later page worked.
func TestAnOffsetOfZeroIsTheFirstPageAndNotAnError(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")
	anna := a.signInAsUser(admin, "Anna", "anna@example.com")

	bookDays(t, anna, 3)

	first := page(t, anna, "?limit=2&offset=0")

	if len(first.Items) != 2 || first.TotalCount != 3 {
		t.Errorf("the first page holds %d entries of a reported %d, want 2 of 3",
			len(first.Items), first.TotalCount)
	}
}

func TestAPageLargerThanTheMaximumIsRefused(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")
	anna := a.signInAsUser(admin, "Anna", "anna@example.com")

	got := anna.api(http.MethodGet, "/timesheets?limit=100000", nil)

	if got.Status != http.StatusBadRequest {
		t.Fatalf("a limit past the maximum answered %d; quietly returning a smaller "+
			"page is the one thing this endpoint's own documentation rules out",
			got.Status)
	}

	if reason := refusalOf(t, got); reason.Code != "pageSizeTooLarge" {
		t.Errorf("the refusal is coded %q, so the interface cannot translate it", reason.Code)
	}
}
