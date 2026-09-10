//go:build browser

package browser

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/dennis-dko/go-time-recording/test/harness"
)

// A shipped role is shown, not opened for changes.
//
// The button said "edit" on all of them, and the server refuses a rename, a
// changed right and now a changed description on the three the application ships
// with. A button offering what the server refuses teaches somebody that the
// screen is broken.
func TestAShippedRoleIsShownRatherThanEdited(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open the roles", p.click(`.tab[data-view="roles"]`),
		chromedp.WaitVisible("#table-roles", chromedp.ByID))

	// The way in says so.
	var offered string

	p.evalJSON(`JSON.stringify(
		[...document.querySelectorAll('#table-roles tbody tr')]
			.filter((row) => row.textContent.includes('Administrator'))
			.map((row) => row.querySelector('button.link')?.textContent ?? '')
			.join('|'))`, &offered)

	if offered == "" {
		t.Fatal("no shipped role has a way in at all")
	}

	if strings.Contains(strings.ToLower(offered), "edit") {
		t.Errorf("the shipped roles offer %q, which the server refuses", offered)
	}

	// And what it opens changes nothing.
	p.run("open it", p.click(`#table-roles tbody tr:nth-child(1) button.link`),
		chromedp.WaitVisible("#form-role", chromedp.ByID))

	for _, field := range []string{"name", "description"} {
		if !p.locked(`#form-role [name="` + field + `"]`) {
			t.Errorf("the %s of a shipped role can be typed into", field)
		}
	}

	var open struct {
		Ticks    int  `json:"ticks"`
		Fixed    int  `json:"fixed"`
		Saveable bool `json:"saveable"`
	}

	p.evalJSON(`JSON.stringify((() => {
		const boxes = [...document.querySelectorAll('#permission-list input[type="checkbox"]')];
		const save = document.querySelector('#form-role button[type="submit"]');

		return {
			ticks: boxes.length,
			fixed: boxes.filter((box) => box.disabled).length,
			saveable: Boolean(save && !save.hidden),
		};
	})())`, &open)

	if open.Ticks == 0 || open.Fixed != open.Ticks {
		t.Errorf("%d of %d rights can still be ticked on a shipped role",
			open.Ticks-open.Fixed, open.Ticks)
	}

	if open.Saveable {
		t.Error("a shipped role is still offered a Save, which the server refuses")
	}

	// Read in the reader's language, since this is a reading screen: the
	// identifier and the English description are what it is stored as.
	if name := p.value(`#form-role [name="name"]`); name == "" {
		t.Error("the role is shown without a name")
	}

	if said := p.value(`#form-role [name="description"]`); said == "" {
		t.Error("the role is shown without a description")
	}
}

// The rights are words, with the identifier beside them and a legend under them.
//
// The boxes said "timesheets:write:own", which asks somebody deciding what a
// colleague may do to read a namespace.
func TestTheRightsAreShownInWordsWithALegend(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open the roles", p.click(`.tab[data-view="roles"]`),
		chromedp.WaitVisible("#permission-list", chromedp.ByID))

	// The list is a container in the markup and its rows arrive with the answer
	// to /permissions, so waiting for the container is waiting for nothing.
	p.waitForNode("#permission-list label")

	var boxes struct {
		Count    int    `json:"count"`
		Named    int    `json:"named"`
		WithID   int    `json:"withId"`
		Grouped  int    `json:"grouped"`
		FirstOne string `json:"first"`
	}

	p.evalJSON(`JSON.stringify((() => {
		const labels = [...document.querySelectorAll('#permission-list label')];

		return {
			count: labels.length,
			named: labels.filter((label) => {
				const name = label.querySelector('.perm-name')?.textContent ?? '';

				return name !== '' && !name.includes(':');
			}).length,
			withId: labels.filter((label) => label.querySelector('.perm-id')).length,
			grouped: document.querySelectorAll('#permission-list .perm-group').length,
			first: labels[0]?.querySelector('.perm-name')?.textContent ?? '',
		};
	})())`, &boxes)

	if boxes.Count == 0 {
		t.Fatal("no rights are offered at all")
	}

	if boxes.Named != boxes.Count {
		t.Errorf("%d of %d rights are still shown as their identifier, e.g. %q",
			boxes.Count-boxes.Named, boxes.Count, boxes.FirstOne)
	}

	// The identifier stays: it is what the API takes and what a directory
	// configuration stores.
	if boxes.WithID != boxes.Count {
		t.Errorf("%d of %d rights no longer show the identifier the API takes",
			boxes.Count-boxes.WithID, boxes.Count)
	}

	if boxes.Grouped < 2 {
		t.Errorf("the rights are in %d group(s), so they are one wall to read",
			boxes.Grouped)
	}

	// The legend, open rather than folded away.
	var legend struct {
		Open    bool `json:"open"`
		Entries int  `json:"entries"`
	}

	p.evalJSON(`JSON.stringify((() => {
		const details = document.querySelector('#permission-legend');

		return {
			open: Boolean(details?.open),
			entries: document.querySelectorAll('#permission-legend-list dd').length,
		};
	})())`, &legend)

	if !legend.Open {
		t.Error("the legend is folded away, so it is read by nobody ticking the boxes")
	}

	if legend.Entries != boxes.Count {
		t.Errorf("the legend explains %d of %d rights", legend.Entries, boxes.Count)
	}
}

// Opening a role puts the rights on the screen, not the table above them.
//
// The form sits a long way down a screen that begins with the table of roles,
// and the legend under the rights made it longer still - so pressing the button
// used to leave somebody looking at the table they had just pressed in, with the
// thing they asked for below the fold.
func TestOpeningARoleShowsItsRights(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open the roles", p.click(`.tab[data-view="roles"]`),
		chromedp.WaitVisible("#table-roles", chromedp.ByID))

	// Before anything is opened: the empty form is the one that makes a role, and
	// its rights have to be settable. They were not - every one of them was
	// rendered with disabled="false", which is an attribute whose presence is its
	// whole meaning, so no role could be granted a right through the interface at
	// all.
	var open int

	p.evalJSON(`JSON.stringify(
		[...document.querySelectorAll('#permission-list input[type="checkbox"]')]
			.filter((box) => !box.disabled).length)`, &open)

	if open == 0 {
		t.Fatal("no right can be set on a new role, so none can be created")
	}

	p.run("back to the top", chromedp.Evaluate(`window.scrollTo(0, 0)`, nil))

	p.run("open a role", p.click(`#table-roles tbody tr:nth-child(1) button.link`),
		chromedp.WaitVisible("#form-role", chromedp.ByID))

	// Given a moment: the scroll is smooth and starts on the next frame.
	var onScreen bool

	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		p.evalJSON(`JSON.stringify((() => {
			const rights = document.querySelector('#form-role .perms').getBoundingClientRect();
			const bar = document.querySelector('.topbar').getBoundingClientRect();

			// On screen and clear of the bar that floats over the top of it.
			return rights.top >= bar.bottom - 1 && rights.top < window.innerHeight;
		})())`, &onScreen)

		if onScreen {
			break
		}

		time.Sleep(150 * time.Millisecond)
	}

	if !onScreen {
		var where float64

		p.evalJSON(`JSON.stringify(
			document.querySelector('#form-role .perms').getBoundingClientRect().top)`, &where)

		t.Errorf("the rights are at %.0f in a %d-tall window after opening the role",
			where, 900)
	}

	// And the fields that cannot be typed into do not offer a typing cursor.
	var cursors string

	p.evalJSON(`JSON.stringify(
		['name', 'description']
			.map((field) => {
				const input = document.querySelector('#form-role [name="' + field + '"]');

				return field + ':' + (input.readOnly ? getComputedStyle(input).cursor : 'writable');
			})
			.join(' '))`, &cursors)

	if strings.Contains(cursors, ":text") {
		t.Errorf("a field that cannot be typed into offers a typing cursor: %s", cursors)
	}

	// The buttons come after the switches and before the legend, with a line of
	// air over them.
	//
	// Under the legend they were a screen and a half below where this jump lands,
	// which is what made the application look unable to create a role at all: the
	// switches of a shipped role are fixed, and the button that unlocks them was
	// nowhere to be seen.
	var layout struct {
		Air          float64 `json:"air"`
		BeforeLegend bool    `json:"beforeLegend"`
		OnScreen     bool    `json:"onScreen"`
	}

	p.evalJSON(`JSON.stringify((() => {
		const rights = document.querySelector('#form-role .perms').getBoundingClientRect();
		const buttons = document.querySelector('#form-role .row');
		const box = buttons.getBoundingClientRect();
		const legend = document.querySelector('#permission-legend');

		return {
			air: box.top - rights.bottom,
			// Earlier in the document than the legend, which is what puts them
			// where the eye lands after the jump.
			beforeLegend: Boolean(
				buttons.compareDocumentPosition(legend) & Node.DOCUMENT_POSITION_FOLLOWING),
			onScreen: box.top < window.innerHeight,
		};
	})())`, &layout)

	if !layout.BeforeLegend {
		t.Error("the buttons are below the legend, which is a screen further down " +
			"than the rights this jumped to")
	}

	if layout.Air < 16 {
		t.Errorf("the buttons sit %.0fpx under the switches", layout.Air)
	}

	// The buttons at the end of the form are off the bottom of the window after
	// this jump, because fifteen switches are taller than a window. So the way
	// out is offered beside the reason instead - which is what somebody reads
	// when they find they cannot set anything.
	//
	// This is the thing that broke: a shipped role opened with everything fixed,
	// and the only control that unlocks it was nowhere on screen. It looked like
	// an application that could no longer create a role.
	var escape struct {
		Shown    bool    `json:"shown"`
		Top      float64 `json:"top"`
		BarBelow float64 `json:"barBelow"`
	}

	p.evalJSON(`JSON.stringify((() => {
		const way = document.querySelector('#role-start-new');
		const box = way.getBoundingClientRect();
		const bar = document.querySelector('.topbar').getBoundingClientRect();

		return { shown: !way.hidden, top: box.top, barBelow: bar.bottom };
	})())`, &escape)

	if !escape.Shown {
		t.Fatal("a shipped role is open with everything fixed and nothing on screen " +
			"offers a way to make a role instead")
	}

	if escape.Top < escape.BarBelow-1 || escape.Top > 900 {
		t.Errorf("the way to a new role is at %.0f, outside the window under the "+
			"bar at %.0f", escape.Top, escape.BarBelow)
	}

	// And taking it leaves a form that can actually be filled in.
	p.run("start a new role", p.click("#role-start-new"))

	var settable int

	p.evalJSON(`JSON.stringify(
		[...document.querySelectorAll('#permission-list input[type="checkbox"]')]
			.filter((box) => !box.disabled).length)`, &settable)

	if settable == 0 {
		t.Error("after asking for a new role the rights still cannot be set")
	}

	if p.locked(`#form-role [name="name"]`) {
		t.Error("after asking for a new role the name still cannot be typed into")
	}
}

// The rights ticked for a role that has not been saved are still ticked.
//
// A set of switches is not a switch. The rights on a role are fifteen boxes all
// carrying the name "permissions" and told apart by their value, and a draft
// that wrote one true or false under that name kept whichever box happened to be
// last - then put every box in the set back agreeing with it. Ticking the rights
// for a new role and reloading came back with none of them.
//
// Two ways of losing them, because they are different faults with the same
// appearance: the reload, and the reload of the screens that happens without one
// - which rebuilds these switches from nothing on every language chosen and
// every neighbouring card saved.
func TestTheRightsTickedForAnUnsavedRoleSurvive(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open Roles", p.click(`.tab[data-view="roles"]`),
		chromedp.WaitVisible("#form-role", chromedp.ByID))
	p.settled()

	const name = "Schichtleitung"

	p.run("name the role", chromedp.SendKeys(
		`#form-role input[name="name"]`, name, chromedp.ByQuery))

	// Whichever the first two are: naming specific rights here would be a case
	// about the catalogue rather than about the switches.
	var ticked string

	p.run("tick two rights", chromedp.Evaluate(`
		(() => {
			const boxes = [...document.querySelectorAll('#permission-list input[name=permissions]')];
			const chosen = boxes.slice(0, 2);

			for (const box of chosen) {
				box.checked = true;
				box.dispatchEvent(new Event('input', { bubbles: true }));
				box.dispatchEvent(new Event('change', { bubbles: true }));
			}

			return JSON.stringify(chosen.map((box) => box.value));
		})()`, &ticked))

	if ticked == "[]" {
		t.Fatal("there are no rights to tick, so this case is about to prove nothing")
	}

	// Under this form's own name, which is not what form.id answers with when a
	// form holds a control called "id" - as this one and the time entry both do.
	// Both wrote to "gtr.draft.[object HTMLInputElement]", one key for two forms,
	// and they overwrote each other on the way out and restored each other's
	// emptiness on the way back in.
	var keys string

	p.run("read the draft keys", chromedp.Evaluate(`
		JSON.stringify(Object.keys(sessionStorage).filter((key) => key.startsWith('gtr.draft.')))`,
		&keys))

	// The account is in the key too, because a draft belongs to whoever wrote it
	// and a browser is shared - see draftsOf. What matters here is the form's own
	// name at the end of it.
	if !strings.Contains(keys, ".form-role") {
		t.Errorf("the role form's draft is not under its own name: %s", keys)
	}

	stillTicked := func(what string) string {
		var got string

		p.run("read the switches "+what, chromedp.Evaluate(`
			JSON.stringify([...document.querySelectorAll('#permission-list input[name=permissions]')]
				.filter((box) => box.checked).map((box) => box.value))`, &got))

		return got
	}

	p.run("reload", chromedp.Reload(), chromedp.WaitVisible("#tabs", chromedp.ByID))
	p.waitGone("#login-screen")
	p.settleWizard()
	p.settled()

	p.run("open Roles again", p.click(`.tab[data-view="roles"]`),
		chromedp.WaitVisible("#form-role", chromedp.ByID))

	if got := p.value(`#form-role input[name="name"]`); got != name {
		t.Errorf("the role is called %q after a reload; %q was typed", got, name)
	}

	if got := stillTicked("after the reload"); got != ticked {
		t.Errorf("the rights read %s after a reload; %s were ticked", got, ticked)
	}

	// And the other way of losing them, which needs no reload at all.
	p.chooseLanguage("de")

	if got := stillTicked("after choosing a language"); got != ticked {
		t.Errorf("the rights read %s after a language was chosen; %s were ticked",
			got, ticked)
	}
}

// The legend that explains the placeholders survives somebody signing out.
//
// Signing out empties every table on the page, so the next person is not handed
// the last one's rows. One of those tables is not data: the legend under
// Appearance listing what may be written in a banner is part of the markup and
// filled by nobody, so once emptied it stayed empty - a heading and a closing
// sentence with the six lines they explain gone from between them.
func TestTheMarkupLegendSurvivesSigningOut(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	rows := func() int {
		var n int

		p.run("count the legend", chromedp.Evaluate(
			`document.querySelectorAll('.markup-table tbody tr').length`, &n))

		return n
	}

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-branding", chromedp.ByID))

	before := rows()

	if before == 0 {
		t.Fatal("the legend is empty before anybody signed out, so this case is " +
			"about to prove nothing")
	}

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn(harness.AdminEmail, "a-much-better-password")
	p.waitGone("#login-screen")
	p.settleWelcome()

	p.run("open Settings again", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-branding", chromedp.ByID))

	if after := rows(); after != before {
		t.Errorf("the legend has %d lines after signing out and back in; it had %d - "+
			"it is markup, and nothing draws it again", after, before)
	}
}
