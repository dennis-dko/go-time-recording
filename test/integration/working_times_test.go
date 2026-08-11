//go:build integration

package integration

import (
	"net/http"
	"testing"
)

// A daily target belongs to the person it is about.
//
// The administrator is responsible for the configuration under Settings and for
// accounts; everything to do with time, each person does for themselves. A daily
// target and a daily ceiling are time figures - the only thing that reads them is the
// overtime balance, which nobody but its owner may see. So an administrator setting
// them was setting a number whose effect it could never look at.
//
// There were three ways in and one of them was locked: PUT /users/{id}/working-times
// asked for a permission, while PUT /users/{id} and the spreadsheet import wrote the
// same two fields on users:write alone. All three are closed, and the one that remains
// asks whose account it is.

// accountsOf lists the accounts as the administrator sees them.
func accountsOf(t *testing.T, admin *client) []struct {
	ID               uint    `json:"id"`
	Email            string  `json:"email"`
	Name             string  `json:"name"`
	DailyTargetHours float64 `json:"dailyTargetHours"`
	MaxDailyHours    float64 `json:"maxDailyHours"`
} {
	t.Helper()

	var people struct {
		Items []struct {
			ID               uint    `json:"id"`
			Email            string  `json:"email"`
			Name             string  `json:"name"`
			DailyTargetHours float64 `json:"dailyTargetHours"`
			MaxDailyHours    float64 `json:"maxDailyHours"`
		} `json:"items"`
	}

	admin.must(admin.api(http.MethodGet, "/users", nil), http.StatusOK).Data(t, &people)

	return people.Items
}

func TestNobodySetsSomebodyElsesWorkingTimes(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")
	mika := a.signInAsUser(admin, "Mika", "mika@example.com")

	var mikaID uint
	var before float64

	for _, person := range accountsOf(t, admin) {
		if person.Email == "mika@example.com" {
			mikaID = person.ID
			before = person.DailyTargetHours
		}
	}

	if mikaID == 0 {
		t.Fatal("Mika is not in the account list")
	}

	// Door one: the working-times route. Refused for somebody else's id, to the
	// account that administers this installation.
	if got := admin.api(http.MethodPut, path("/users/", mikaID)+"/working-times",
		map[string]any{"dailyTargetHours": 4, "maxDailyHours": 5}).Status; got !=
		http.StatusForbidden {
		t.Errorf("the administrator set somebody else's working times: status %d, want 403", got)
	}

	// Door two: the account editor, which took the same two fields on users:write.
	// It answers 200 because renaming is allowed - what must not happen is the
	// figures moving, which is checked below.
	admin.must(admin.api(http.MethodPut, path("/users/", mikaID), map[string]any{
		"dailyTargetHours": 4, "maxDailyHours": 5,
	}), http.StatusOK)

	for _, person := range accountsOf(t, admin) {
		if person.Email != "mika@example.com" {
			continue
		}

		if person.DailyTargetHours != before {
			t.Errorf("the account editor moved the daily target from %v to %v",
				before, person.DailyTargetHours)
		}
	}

	// And it is Mika's to set.
	mika.must(mika.api(http.MethodPut, path("/users/", mikaID)+"/working-times",
		map[string]any{"dailyTargetHours": 6, "maxDailyHours": 9}), http.StatusOK)

	for _, person := range accountsOf(t, admin) {
		if person.Email == "mika@example.com" && person.DailyTargetHours != 6 {
			t.Errorf("Mika's own change did not take: the target is %v, want 6",
				person.DailyTargetHours)
		}
	}
}

// A new account starts on the instance default rather than on whatever the form said.
//
// The create form carried the two figures, which made it a fourth way for somebody
// else to decide them. It no longer does, and the fields are gone from the screen -
// so a body that still sends them changes nothing.
func TestANewAccountStartsOnTheInstanceDefault(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	admin.must(admin.api(http.MethodPost, "/users", map[string]any{
		"name": "Nina", "email": "nina@example.com", "role": "user",
		"password": "nina-password-1",
		// Ignored, and this is the assertion: an old client sending them must not
		// decide somebody's working time.
		"dailyTargetHours": 3, "maxDailyHours": 4,
	}), http.StatusCreated, http.StatusOK)

	for _, person := range accountsOf(t, admin) {
		if person.Email != "nina@example.com" {
			continue
		}

		if person.DailyTargetHours == 3 || person.MaxDailyHours == 4 {
			t.Errorf("the create form decided somebody's working times: %v/%v",
				person.DailyTargetHours, person.MaxDailyHours)
		}

		// Zero, which is how "follow the instance default" is stored and what the
		// screen shows as "default". Not resolved in the response on purpose: a
		// resolved eight cannot be told from a chosen eight, and the person who has
		// not chosen would be offered a figure to clear that was never set.
		if person.DailyTargetHours != 0 || person.MaxDailyHours != 0 {
			t.Errorf("the new account was given working times of its own: %v/%v",
				person.DailyTargetHours, person.MaxDailyHours)
		}

		return
	}

	t.Error("the new account is not in the list")
}

// A personal daily ceiling actually stops a booking, and cannot loosen the
// installation's.
//
// It was stored, offered on screen, and read by nothing: only the instance ceiling was
// ever consulted. A field that changes nothing when you fill it in is worse than a
// field that is not there.
//
// The stricter of the two applies, and both halves of that matter. Somebody may hold
// their own day shorter than the installation allows - that is their time. They may not
// raise it past what the installation allows, because that ceiling is configuration and
// configuration is not theirs.
func TestAPersonalDailyCeilingHoldsAndCannotLoosenTheInstanceOne(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")
	mika := a.signInAsUser(admin, "Mika", "mika@example.com")

	var mikaID uint

	for _, person := range accountsOf(t, admin) {
		if person.Email == "mika@example.com" {
			mikaID = person.ID
		}
	}

	if mikaID == 0 {
		t.Fatal("Mika is not in the account list")
	}

	// Tighter than the installation's, which is what somebody choosing a shorter day
	// looks like.
	mika.must(mika.api(http.MethodPut, path("/users/", mikaID)+"/working-times",
		map[string]any{"dailyTargetHours": 4, "maxDailyHours": 5}), http.StatusOK)

	mika.must(mika.api(http.MethodPost, "/timesheets", map[string]any{
		"date": "2026-08-10", "durationHours": 4,
	}), http.StatusCreated, http.StatusOK)

	// Four booked, five allowed: two more is over.
	over := mika.api(http.MethodPost, "/timesheets", map[string]any{
		"date": "2026-08-10", "durationHours": 2,
	})

	if over.Status != http.StatusConflict {
		t.Errorf("booking past a personal daily ceiling answered %d, want 409", over.Status)
	}

	// And one more hour is inside it.
	mika.must(mika.api(http.MethodPost, "/timesheets", map[string]any{
		"date": "2026-08-10", "durationHours": 1,
	}), http.StatusCreated, http.StatusOK)

	// Now the other half. The administrator sets the installation's ceiling, which is
	// its job, and Mika sets a personal one above it - which must not raise anything.
	admin.must(admin.api(http.MethodPut, "/settings/operational",
		map[string]any{"maxDailyHours": 6}), http.StatusOK)

	mika.must(mika.api(http.MethodPut, path("/users/", mikaID)+"/working-times",
		map[string]any{"dailyTargetHours": 8, "maxDailyHours": 20}), http.StatusOK)

	tooMuch := mika.api(http.MethodPost, "/timesheets", map[string]any{
		"date": "2026-08-11", "durationHours": 9,
	})

	if tooMuch.Status != http.StatusConflict {
		t.Errorf("a personal ceiling of 20 raised the installation's 6: booking 9h "+
			"answered %d, want 409", tooMuch.Status)
	}

	// Inside both, so this cannot pass by everything being refused.
	mika.must(mika.api(http.MethodPost, "/timesheets", map[string]any{
		"date": "2026-08-11", "durationHours": 5,
	}), http.StatusCreated, http.StatusOK)
}
