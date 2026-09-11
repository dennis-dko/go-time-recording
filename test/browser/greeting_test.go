//go:build browser

package browser

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// The greeting is a screen, reachable from the title, and it says what today has
// on it.
//
// It used to be two half-measures: a modal on a first sign-in, and a card wedged
// above the time entries afterwards. Neither could be gone back to - the only ways
// to read the greeting were to be new or to have just arrived - which is why it is
// a screen now, and why the title in the header leads to it from anywhere.
func TestTheGreetingIsAScreenReachableFromTheTitle(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("create a user",
		chromedp.Click(`.tab[data-view="users"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#form-user", chromedp.ByID),
		chromedp.SendKeys(`#form-user input[name="name"]`, "Sven", chromedp.ByQuery),
		chromedp.SendKeys(`#form-user input[name="email"]`, "sven@example.com", chromedp.ByQuery),
		p.chooseOption(`#form-user select[name="role"]`, "user"),
		chromedp.SendKeys(`#form-user input[name="password"]`, "sven-password-1", chromedp.ByQuery),
		p.click(`#form-user button[type="submit"]`),
	)

	time.Sleep(500 * time.Millisecond)

	p.run("sign out", p.click("#logout"), chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn("sven@example.com", "sven-password-1")
	p.waitGone("#login-screen")

	// The walk through, out of the way: what this case is about is the screen
	// behind it.
	p.settleWelcome()

	// Somewhere else entirely, so that arriving at the greeting is a navigation
	// rather than where the page happened to be.
	p.run("go to the calendar",
		chromedp.Click(`.tab[data-view="calendar"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#calendar-days", chromedp.ByID))

	p.run("press the title", p.click("#app-title"),
		chromedp.WaitVisible("#view-welcome", chromedp.ByID))

	if title := p.text("#welcome-title"); !strings.Contains(title, "Sven") {
		t.Errorf("the greeting does not name the person: %q", title)
	}

	// It says something about today rather than only hello - what somebody would
	// otherwise have to go and look up.
	if today := p.text("#welcome-today"); today == "" {
		t.Error("the greeting says nothing about today")
	}

	// The points offered are the ones this person can act on, and there is
	// something there at all: the list is built from permissions, so a near-empty
	// greeting means the building went wrong rather than that there is little to
	// say.
	points := p.text("#welcome-points")

	if len(points) < 60 {
		t.Errorf("the greeting lists almost nothing this person can do: %q", points)
	}

	if strings.Contains(strings.ToLower(points), "genehmig") {
		t.Errorf("the greeting promises approvals, which nobody does any more: %q", points)
	}

	// And it is a screen you leave the way you leave any other.
	p.run("carry on", p.click("#welcome-continue"))
	p.waitGone("#view-welcome")
}

// The greeting shows the last few entries to somebody who books time.
//
// A greeting that says nothing about the work is one nobody reads twice. And it
// is hidden outright for an account that records none: the built-in
// administrator has no entries, and a panel explaining that is worse than no
// panel.
func TestTheGreetingShowsTheLastEntries(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	// The account that only administers sees no panel at all.
	p.run("go to the greeting", p.click("#app-title"),
		chromedp.WaitVisible("#welcome-title", chromedp.ByID))

	if p.visible("#welcome-recent") {
		t.Error("the account that records no time is shown a panel of its entries")
	}

	// Somebody who does book time, with something to show.
	p.becomeWorker()
	p.bookAnHourOn(t, "Dachsanierung")
	p.bookAnHourOn(t, "Serverumzug")

	p.run("reload", chromedp.Reload(), chromedp.WaitVisible("#who", chromedp.ByID))
	p.run("go to the greeting", p.click("#app-title"),
		chromedp.WaitVisible("#welcome-title", chromedp.ByID))

	p.waitForNode("#welcome-recent-list li")

	if !p.visible("#welcome-recent") {
		t.Fatal("somebody who books time is shown no entries on the greeting")
	}

	// A card of its own, under the greeting rather than inside it: one card
	// introduces the application and one reports the work, and the second is the
	// one that stays useful after the first week.
	var standing struct {
		OwnCard bool    `json:"ownCard"`
		Below   float64 `json:"below"`
	}

	p.evalJSON(`JSON.stringify((() => {
		const panel = document.querySelector('#welcome-recent');
		const greeting = document.querySelector('#view-welcome .card');

		return {
			ownCard: panel.classList.contains('card') && !greeting.contains(panel),
			below: panel.getBoundingClientRect().top - greeting.getBoundingClientRect().bottom,
		};
	})())`, &standing)

	if !standing.OwnCard {
		t.Error("the entries sit inside the greeting card rather than in one of their own")
	}

	if standing.Below < 0 {
		t.Errorf("the entries card overlaps the greeting by %.0fpx", -standing.Below)
	}

	var rows struct {
		Count    int      `json:"count"`
		Projects []string `json:"projects"`
		Hours    []string `json:"hours"`
		Empty    bool     `json:"empty"`
	}

	p.evalJSON(`JSON.stringify((() => {
		const list = [...document.querySelectorAll('#welcome-recent-list li')];

		return {
			count: list.length,
			projects: list.map((row) => row.querySelector('.welcome-recent-project')?.textContent ?? ''),
			hours: list.map((row) => row.querySelector('.welcome-recent-hours')?.textContent ?? ''),
			empty: !document.querySelector('#welcome-recent-empty').hidden,
		};
	})())`, &rows)

	if rows.Count != 2 {
		t.Errorf("the greeting lists %d entries, want the 2 that were booked", rows.Count)
	}

	if rows.Empty {
		t.Error("the greeting says nothing was recorded while listing entries")
	}

	// Named, not numbered: a row saying "7" for a project helps nobody.
	for _, project := range rows.Projects {
		if project == "" || strings.TrimSpace(project) == "" {
			t.Errorf("an entry is listed without a project: %v", rows.Projects)
		}
	}

	for _, hours := range rows.Hours {
		if !strings.Contains(hours, "1") {
			t.Errorf("an hour was booked and the row reads %q", hours)
		}
	}

	// And the way through to all of them.
	p.run("all entries", p.click("#welcome-recent-all"),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID))

	if !p.visible("#table-timesheets") {
		t.Error("the way to all entries does not lead to them")
	}
}
