//go:build integration

package integration

import (
	"net/http"
	"testing"
	"time"
)

// The clock is the one way into the timesheet table that does not go through a
// form, so the thing worth checking is that it lands on the same rules: the daily
// cap, the project having to exist and be visible, the description length. A
// second path that enforced its own subset of those is how two paths drift apart.

// TimerOnTheWire mirrors the response.
type TimerOnTheWire struct {
	Running      bool    `json:"running"`
	ProjectID    *uint   `json:"projectId"`
	Description  *string `json:"description"`
	StartedAt    *string `json:"startedAt"`
	ElapsedHours float64 `json:"elapsedHours"`
}

func runningTimer(t *testing.T, c *client) TimerOnTheWire {
	t.Helper()

	var timer TimerOnTheWire

	c.must(c.api(http.MethodGet, "/me/timer", nil), http.StatusOK).Data(t, &timer)

	return timer
}

// Nothing is running on a fresh account, and asking is not an error.
func TestNoTimerIsRunningToBeginWith(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	if timer := runningTimer(t, admin); timer.Running {
		t.Errorf("a timer is already running: %+v", timer)
	}
}

// Starting records the clock and says when it started, so the interface can count
// up on its own rather than asking every second to be told the same thing.
func TestStartingAndReadingBackTheClock(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	// Private: the clock is the caller's own, and a private project is what the
	// administrator may create - the shared ones belong to whoever runs the work.
	var project projectResponse
	admin.must(admin.api(http.MethodPost, "/projects", map[string]any{
		"name": "Timed work", "startDate": "2026-08-01", "private": true,
	}), http.StatusCreated, http.StatusOK).Data(t, &project)

	description := "Writing the timer"

	var started TimerOnTheWire
	admin.must(admin.api(http.MethodPost, "/me/timer", map[string]any{
		"projectId": project.ID, "description": description,
	}), http.StatusCreated, http.StatusOK).Data(t, &started)

	if !started.Running {
		t.Fatal("starting a timer did not report it as running")
	}

	if started.StartedAt == nil || *started.StartedAt == "" {
		t.Error("the response does not say when it started")
	}

	timer := runningTimer(t, admin)

	if !timer.Running {
		t.Fatal("the timer is not running when read back")
	}

	if timer.ProjectID == nil || *timer.ProjectID != project.ID {
		t.Errorf("the timer points at %v, want project %d", timer.ProjectID, project.ID)
	}

	if timer.Description == nil || *timer.Description != description {
		t.Errorf("the description came back as %v", timer.Description)
	}
}

// Starting a second time replaces the first. Somebody who does that has changed
// their mind about what they are doing, and refusing would leave them to stop a
// clock that is measuring the wrong thing.
func TestStartingAgainReplacesTheRunningClock(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	admin.must(admin.api(http.MethodPost, "/me/timer",
		map[string]any{"description": "first"}), http.StatusCreated, http.StatusOK)
	admin.must(admin.api(http.MethodPost, "/me/timer",
		map[string]any{"description": "second"}), http.StatusCreated, http.StatusOK)

	timer := runningTimer(t, admin)

	if timer.Description == nil || *timer.Description != "second" {
		t.Errorf("the running clock says %v, want the second one", timer.Description)
	}
}

// Discarding leaves no record at all, which is the way out of a clock nobody
// meant to start.
func TestDiscardingATimerRecordsNothing(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	before := timesheetCount(t, admin)

	admin.must(admin.api(http.MethodPost, "/me/timer", nil), http.StatusCreated, http.StatusOK)
	admin.must(admin.api(http.MethodDelete, "/me/timer", nil),
		http.StatusNoContent, http.StatusOK)

	if timer := runningTimer(t, admin); timer.Running {
		t.Error("the timer is still running after being discarded")
	}

	if after := timesheetCount(t, admin); after != before {
		t.Errorf("discarding created %d entr(y/ies)", after-before)
	}
}

// A clock stopped straight away has measured almost nothing, and rounding that up
// to the smallest bookable duration would record work nobody did. Refused, with
// the way out in the message.
func TestStoppingImmediatelyIsRefusedRatherThanRoundedUp(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	before := timesheetCount(t, admin)

	admin.must(admin.api(http.MethodPost, "/me/timer", nil), http.StatusCreated, http.StatusOK)

	response := admin.api(http.MethodPost, "/me/timer/stop", nil)
	if response.Status == http.StatusOK || response.Status == http.StatusCreated {
		t.Fatalf("a timer running for milliseconds was booked: %s", response.Body)
	}

	if response.Message() == "" {
		t.Error("the refusal says nothing about what to do instead")
	}

	if after := timesheetCount(t, admin); after != before {
		t.Errorf("the refused stop created %d entr(y/ies)", after-before)
	}

	// And the clock is still there, so the time is not lost to the refusal.
	if timer := runningTimer(t, admin); !timer.Running {
		t.Error("the refused stop threw the clock away")
	}
}

// The one that matters: a clock that has run long enough becomes an ordinary time
// entry, with the measured duration and not a rounded one.
func TestStoppingBooksTheMeasuredTime(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	admin.must(admin.api(http.MethodPost, "/me/timer",
		map[string]any{"description": "measured"}), http.StatusCreated, http.StatusOK)

	// Past the smallest bookable duration, which is what stops a clock started by
	// accident from becoming a record.
	time.Sleep(40 * time.Second)

	var entry timesheetResponse
	admin.must(admin.api(http.MethodPost, "/me/timer/stop", nil),
		http.StatusCreated, http.StatusOK).Data(t, &entry)

	// Around forty seconds, and emphatically not a quarter of an hour.
	if entry.DurationHours < 0.01 || entry.DurationHours > 0.05 {
		t.Errorf("the entry records %v hours, want roughly 40 seconds", entry.DurationHours)
	}

	if entry.Description == nil || *entry.Description != "measured" {
		t.Errorf("the description did not travel: %v", entry.Description)
	}

	// The clock is gone, so stopping twice cannot double-count.
	if timer := runningTimer(t, admin); timer.Running {
		t.Error("the clock survived being stopped")
	}

	if got := admin.api(http.MethodPost, "/me/timer/stop", nil).Status; got == http.StatusOK ||
		got == http.StatusCreated {
		t.Error("stopping again booked a second entry")
	}
}

// The rules a typed booking meets, met by a timer booking too. A project that does
// not exist is the cheapest of them to provoke, and it proves the entry goes
// through the same service rather than straight to the table.
// A project that stops accepting time while the clock is running.
//
// This used to be written by starting a timer on a project id that does not exist,
// which SQLite allowed and PostgreSQL refused outright - the start is now checked,
// so that state cannot be reached at all. The rule being tested is the same one:
// when the entry would be refused, the refusal must not take the measured time with
// it. Reached here through a project that is completed while the clock runs, which
// is a thing that really happens.
func TestStoppingIsRefusedWhenTheEntryWouldBe(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")
	other := a.signInAsUser(admin, "Nils", "nils@example.com")

	var project projectResponse
	other.must(other.api(http.MethodPost, "/projects", map[string]any{
		"name": "Finishing soon", "startDate": "2026-08-01",
	}), http.StatusCreated, http.StatusOK).Data(t, &project)

	other.must(other.api(http.MethodPost, "/me/timer",
		map[string]any{"projectId": project.ID}), http.StatusCreated, http.StatusOK)

	time.Sleep(40 * time.Second)

	// Completed while the clock is still running, so the entry it would create is
	// one the booking rules refuse.
	other.must(other.api(http.MethodPut, path("/projects/", project.ID), map[string]any{
		"name": "Finishing soon", "startDate": "2026-08-01", "status": "completed",
	}), http.StatusOK)

	response := other.api(http.MethodPost, "/me/timer/stop", nil)
	if response.Status == http.StatusOK || response.Status == http.StatusCreated {
		t.Errorf("a timer on a project that no longer accepts time was booked: %s",
			response.Body)
	}

	// The clock stays, so the measured time survives a refusal the user can act
	// on - they can point it somewhere real and stop it again.
	if timer := runningTimer(t, other); !timer.Running {
		t.Error("the refused stop threw the clock away, losing the time")
	}
}

// Starting the clock against a project that does not exist is refused there and
// then, rather than stored and discovered later.
//
// Two behaviours were hiding behind SQLite's tolerance of foreign keys: the row was
// inserted, leaving a clock that could never be stopped, while PostgreSQL rejected
// the insert and answered 500 for what is a mistyped number. Now it is a 404 on
// every engine, and nothing is stored.
func TestStartingTheClockOnAProjectThatDoesNotExistIsRefused(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	refused := admin.api(http.MethodPost, "/me/timer", map[string]any{"projectId": 999999})

	if refused.Status != http.StatusNotFound {
		t.Errorf("starting a clock on a project that does not exist answered %d, want 404: %s",
			refused.Status, refused.Body)
	}

	// And no clock was left behind, which is the half SQLite used to allow.
	if timer := runningTimer(t, admin); timer.Running {
		t.Error("a clock was started against a project that does not exist")
	}
}

// Somebody else's private project is not a project you can time against, and it is
// reported the same way it is everywhere else: not found, so its existence stays
// private.
func TestStartingTheClockOnSomebodyElsesPrivateProjectIsRefused(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	admin.must(admin.api(http.MethodPost, "/users", map[string]any{
		"name": "Ove", "email": "ove@example.com",
		"role": "employee", "password": "ove-password-1",
	}), http.StatusCreated, http.StatusOK)

	ove := a.newClient()
	ove.signIn("ove@example.com", "ove-password-1")

	var hers projectResponse
	ove.must(ove.api(http.MethodPost, "/projects", map[string]any{
		"name": "Ove's own", "startDate": "2026-08-01", "private": true,
	}), http.StatusCreated, http.StatusOK).Data(t, &hers)

	if got := admin.api(http.MethodPost, "/me/timer",
		map[string]any{"projectId": hers.ID}).Status; got != http.StatusNotFound {
		t.Errorf("timing against somebody else's private project answered %d, want 404", got)
	}

	if timer := runningTimer(t, admin); timer.Running {
		t.Error("a clock was started against somebody else's private project")
	}
}

// timesheetCount is how many entries the caller can see, for the before-and-after
// assertions above.
func timesheetCount(t *testing.T, c *client) int {
	t.Helper()

	var list listOf[timesheetResponse]

	c.must(c.api(http.MethodGet, "/timesheets", nil), http.StatusOK).Data(t, &list)

	return len(list.Items)
}

// A running clock must not stop an account or a project from being removed, and
// must not survive either of them.
//
// running_timers references both users(id) and projects(id) with no ON DELETE
// behaviour declared, so a row left pointing at something that has gone is a
// foreign key violation on PostgreSQL and MySQL - a 500 for an administrator
// deleting an account - and on SQLite a clock nobody can ever stop. The same shape
// as timing against a project that never existed, reached from the other end.

func TestDeletingAnAccountWithAClockRunning(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	var created struct {
		ID uint `json:"id"`
	}

	admin.must(admin.api(http.MethodPost, "/users", map[string]any{
		"name": "Pia", "email": "pia@example.com",
		"role": "employee", "password": "pia-password-1",
	}), http.StatusCreated, http.StatusOK).Data(t, &created)

	pia := a.newClient()
	pia.signIn("pia@example.com", "pia-password-1")

	pia.must(pia.api(http.MethodPost, "/me/timer", map[string]any{
		"description": "still running when the account goes",
	}), http.StatusCreated, http.StatusOK)

	// The account has no recorded time, so this is the plain deletion path.
	deleted := admin.api(http.MethodDelete, path("/users/", created.ID), nil)

	if deleted.Status != http.StatusOK && deleted.Status != http.StatusNoContent {
		t.Fatalf("deleting an account with a clock running answered %d: %s\n\n"+
			"application log:\n%s", deleted.Status, deleted.Body, a.log())
	}

	if got := admin.api(http.MethodGet, path("/users/", created.ID), nil).Status; got != http.StatusNotFound {
		t.Errorf("the deleted account reads as %d, want 404", got)
	}
}

func TestDeletingAProjectSomebodyIsTimingAgainst(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")
	other := a.signInAsUser(admin, "Rolf", "rolf@example.com")

	var project projectResponse
	other.must(other.api(http.MethodPost, "/projects", map[string]any{
		"name": "Timed against", "startDate": "2026-08-01",
	}), http.StatusCreated, http.StatusOK).Data(t, &project)

	other.must(other.api(http.MethodPost, "/me/timer",
		map[string]any{"projectId": project.ID}), http.StatusCreated, http.StatusOK)

	// The project has no entries, so nothing else stands in the way of deleting it.
	deleted := other.api(http.MethodDelete, path("/projects/", project.ID), nil)

	switch deleted.Status {
	case http.StatusOK, http.StatusNoContent:
		// Accepted. Then nothing may be left pointing at a project that is gone.
		if timer := runningTimer(t, other); timer.Running && timer.ProjectID != nil {
			t.Errorf("the project was deleted and a clock still points at it (%d)",
				*timer.ProjectID)
		}

	case http.StatusConflict:
		// Refused, which is the same answer a project with entries gets. Then it
		// has to say so, and the project has to still be there.
		if deleted.Message() == "" {
			t.Error("the deletion was refused without saying why")
		}

		if got := other.api(http.MethodGet, path("/projects/", project.ID), nil).Status; got != http.StatusOK {
			t.Errorf("the refused deletion left the project reading as %d, want 200", got)
		}

	default:
		t.Fatalf("deleting a project somebody is timing against answered %d: %s\n\n"+
			"Neither an acceptance nor an explained refusal - on an engine that "+
			"enforces the foreign key this is the constraint surfacing as an "+
			"internal error.\n\napplication log:\n%s",
			deleted.Status, deleted.Body, a.log())
	}
}
