//go:build browser

package browser

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

const (
	secondEmail    = "bela@example.com"
	secondPassword = "bela-password-1"
)

// The calendar opens on this account's own month, not on where the last one left
// it.
//
// handBackTheScreen clears what belonged to whoever is leaving, and it is
// thorough about it: the drafts, every form, the loose fields marked data-keep,
// the caches, the tables, the filled lists, the appearance, the language, the
// release and restart banners, the rights notice. Each of those paragraphs was
// written after somebody found the previous person's something still on screen.
//
// calendarMonth is module state of exactly that kind and is not in the list. It
// is set by the arrows, so a person who paged back to March and signed out left
// March behind, and the next account at that desk opened its calendar there.
//
// Its own doc comment says why the month is worked out late rather than early:
// "which month somebody is in is a question about their own zone, and the zone is
// not known until they are signed in", and an early answer opened an Auckland
// account on the month it had just left "with today highlighted nowhere in the
// grid". A memo held across a sign-out is that same wrong answer, arrived at from
// the other direction.
func TestTheCalendarOpensOnTheNewAccountsMonth(t *testing.T) {
	t.Parallel()

	p := open(t)

	// Two ordinary accounts, because the built-in administrator has no calendar:
	// timesheets:read:own is deliberately revoked from it - it runs the
	// installation rather than booking time - so the tab it would be clicked on
	// does not exist for it.
	p.readyAdmin()
	p.createOrdinaryAccount(t, secondEmail, secondPassword)
	p.becomeWorker()

	p.run("open the calendar", p.click(`.tab[data-view="calendar"]`),
		chromedp.WaitVisible("#calendar-title", chromedp.ByID))

	thisMonth := p.waitForCalendarMonth(t, "")

	p.run("page back to another month", p.click("#calendar-prev"))

	paged := p.waitForCalendarMonth(t, thisMonth)

	if paged == thisMonth {
		t.Fatalf("the arrow did not change the month (still %q), so this case would "+
			"pass whatever the sign-out does", thisMonth)
	}

	p.run("sign out", p.click("#logout"),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	// A different account at the same desk, which is what the paragraphs in
	// handBackTheScreen are each about.
	p.signIn(secondEmail, secondPassword)
	p.waitGone("#login-screen")
	p.settled()

	// The walk-through opens by itself for an account that has never seen it, and
	// it covers the tabs - so without this the click below lands on the overlay
	// and the case fails as a timeout that says nothing about the calendar.
	p.settleWelcome()

	p.run("open the calendar again", p.click(`.tab[data-view="calendar"]`),
		chromedp.WaitVisible("#calendar-title", chromedp.ByID))

	opened := p.waitForCalendarMonth(t, "")

	if opened == paged {
		t.Errorf("the calendar opened on %q - the month the previous account had "+
			"paged to - rather than on %q. calendarMonth survives the sign-out that "+
			"clears the drafts, the forms, the caches and the tables", paged, thisMonth)

		return
	}

	if opened != thisMonth {
		t.Errorf("the calendar opened on %q rather than on this account's own "+
			"month %q", opened, thisMonth)
	}
}

// waitForCalendarMonth reads the month heading once the grid has been drawn,
// optionally waiting for it to become something other than `changingFrom`.
func (p *page) waitForCalendarMonth(t *testing.T, changingFrom string) string {
	t.Helper()

	deadline := time.Now().Add(waitPatience)

	var shown string

	for time.Now().Before(deadline) {
		p.run("read the month", chromedp.Evaluate(
			`(document.querySelector('#calendar-title')?.textContent ?? '').trim()`,
			&shown))

		// "Calendar" is what the markup carries until the grid is rendered over it.
		if shown != "" && shown != "Calendar" && shown != changingFrom {
			return shown
		}

		time.Sleep(200 * time.Millisecond)
	}

	t.Fatalf("the calendar heading never settled (last read %q)", shown)

	return ""
}
