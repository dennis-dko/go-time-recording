//go:build integration

package integration

import (
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/pkg/security"
)

// Two people saving at the same moment must both be served.
//
// SQLite serialises writers, and without a busy timeout the second one is refused
// outright with "database is locked (5) (SQLITE_BUSY)" - a 500 for somebody whose
// only mistake was saving while somebody else saved. Write-ahead logging fixed the
// reader-versus-writer half of that; this is the writer-versus-writer half, which
// a connection hook now covers by making them queue.
//
// Found for real: signing in became a write of its own when the greeting was
// added, and the first action after a sign-in started colliding with it. The window
// was small enough that it took a browser suite under load to show it, which is
// exactly the kind of failure that reaches a person and never a test.

// The default dialect is SQLite, so this runs against it unless GTR_TEST_DSN
// points somewhere else - where it is still a fair test of concurrent writes.
func TestConcurrentWritesAreServedRatherThanRefused(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	// Several accounts, each with its own session, so the writes are genuinely
	// concurrent rather than serialised by one client.
	const people = 6

	clients := make([]*client, 0, people)

	for i := 0; i < people; i++ {
		email := fmt.Sprintf("writer%d@example.com", i)

		admin.must(admin.api(http.MethodPost, "/users", map[string]any{
			"name": fmt.Sprintf("Writer %d", i), "email": email,
			"role": "user", "password": "writer-password-1",
		}), http.StatusCreated, http.StatusOK)

		c := a.newClient()
		c.signIn(email, "writer-password-1")
		clients = append(clients, c)
	}

	// All of them book at once, twice over, which is the shape that used to fail.
	const each = 3

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		refused []string
	)

	for i, c := range clients {
		for attempt := 0; attempt < each; attempt++ {
			wg.Add(1)

			go func(c *client, person, attempt int) {
				defer wg.Done()

				r := c.api(http.MethodPost, "/timesheets", map[string]any{
					"date": "2026-08-03", "durationHours": 1,
					"description": fmt.Sprintf("writer %d, booking %d", person, attempt),
				})

				if r.Status == http.StatusCreated || r.Status == http.StatusOK {
					return
				}

				mu.Lock()
				refused = append(refused,
					fmt.Sprintf("writer %d booking %d: %d %s", person, attempt, r.Status, r.Body))
				mu.Unlock()
			}(c, i, attempt)
		}
	}

	wg.Wait()

	if len(refused) > 0 {
		t.Errorf("%d of %d concurrent bookings were refused:\n%v\n\napplication log:\n%s",
			len(refused), people*each, refused, a.log())
	}

	// And every one of them is really there: a booking that answered 201 and then
	// lost the row would be worse than a refusal.
	//
	// Asked of each person about their own, because that is all anybody can ask. It
	// also checks the arithmetic per account rather than in one total, so three rows
	// landing under the wrong writer would show up here instead of cancelling out.
	for i, c := range clients {
		var listed listOf[timesheetResponse]

		c.must(c.api(http.MethodGet, "/timesheets?from=2026-08-03&to=2026-08-03", nil),
			http.StatusOK).Data(t, &listed)

		if len(listed.Items) != each {
			t.Errorf("writer %d sees %d of its %d bookings", i, len(listed.Items), each)
		}
	}
}

// A write racing a sign-in, which is the collision the greeting introduced: it
// records that the introduction has been seen, and the next thing the person does
// is another write.
func TestAWriteRightAfterSigningInIsServed(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	admin.must(admin.api(http.MethodPost, "/users", map[string]any{
		"name": "Tarek", "email": "tarek@example.com",
		"role": "user", "password": "tarek-password-1",
	}), http.StatusCreated, http.StatusOK)

	for attempt := 0; attempt < 5; attempt++ {
		c := a.newClient()
		c.signIn("tarek@example.com", "tarek-password-1")

		var wg sync.WaitGroup

		var tourStatus, bookStatus int

		wg.Add(2)

		// What the interface does on a first sign-in...
		go func() {
			defer wg.Done()

			tourStatus = c.api(http.MethodPut, "/me/tour",
				map[string]any{"seen": true}).Status
		}()

		// ...and what the person does next, at the same moment.
		go func() {
			defer wg.Done()

			bookStatus = c.api(http.MethodPost, "/timesheets", map[string]any{
				"date": "2026-08-04", "durationHours": 1,
			}).Status
		}()

		wg.Wait()

		if tourStatus != http.StatusOK {
			t.Errorf("attempt %d: recording the tour answered %d", attempt, tourStatus)
		}

		if bookStatus != http.StatusCreated && bookStatus != http.StatusOK {
			t.Errorf("attempt %d: the booking beside it answered %d\n\napplication log:\n%s",
				attempt, bookStatus, a.log())
		}
	}
}

// One setting written in passing must not take another one with it.
//
// Every write to the account went through an update of the whole row: read it,
// change one field, write all the columns back. Two of those at once and the
// second one silently reverts the first.
//
// Found by a browser test that could no longer enable two-factor: recording the
// guided tour as seen - which the greeting does the moment somebody signs in - had
// read the row before the enrolment issued a secret, and wrote it back without one.
// The enrolment then answered 409, because as far as the server was concerned no
// enrolment had been started.
func TestASettingWrittenInPassingDoesNotRevertAnother(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	admin.must(admin.api(http.MethodPost, "/users", map[string]any{
		"name": "Yusuf", "email": "yusuf@example.com",
		"role": "user", "password": "yusuf-password-1",
	}), http.StatusCreated, http.StatusOK)

	// Repeated, because the order of two concurrent writes is not fixed and the
	// bug only shows in one of them.
	for attempt := 0; attempt < 5; attempt++ {
		c := a.newClient()
		c.signIn("yusuf@example.com", "yusuf-password-1")

		var wg sync.WaitGroup

		var setup enrolment

		var tourStatus int

		wg.Add(2)

		// The enrolment issues a secret and stores it on the account...
		go func() {
			defer wg.Done()

			setup = beginEnrolment(t, c)
		}()

		// ...while the greeting records itself as seen, on the same account.
		go func() {
			defer wg.Done()

			tourStatus = c.api(http.MethodPut, "/me/tour",
				map[string]any{"seen": true}).Status
		}()

		wg.Wait()

		if tourStatus != http.StatusOK {
			t.Fatalf("attempt %d: recording the tour answered %d", attempt, tourStatus)
		}

		if setup.Secret == "" {
			t.Fatalf("attempt %d: no secret was issued", attempt)
		}

		// The secret has to still be there. Asked through the enrolment itself:
		// beginning a second one is refused once two-factor is on, and until then
		// what matters is that the pending secret survived - which the next
		// enrolment replacing it would also prove, so this checks the account
		// instead.
		var me struct {
			User userResponse `json:"user"`
		}

		c.must(c.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &me)

		if !me.User.TourSeen {
			t.Errorf("attempt %d: the tour flag was reverted by the enrolment", attempt)
		}

		// And the enrolment can be confirmed, which is only possible if the secret
		// stored on the account is still the one that was issued.
		code, err := security.CurrentTOTPCode(setup.Secret)
		if err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}

		confirmed := c.api(http.MethodPut, "/me/totp", map[string]any{"code": code})
		if confirmed.Status != http.StatusOK {
			t.Fatalf("attempt %d: confirming the enrolment answered %d: %s\n\n"+
				"the secret issued a moment earlier is gone, which is the lost update",
				attempt, confirmed.Status, confirmed.Body)
		}

		// Turned off again so the next attempt starts from the same place.
		off, err := security.CurrentTOTPCode(setup.Secret)
		if err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}

		c.must(c.api(http.MethodDelete, "/me/totp?code="+off, nil),
			http.StatusOK, http.StatusNoContent)
	}
}
