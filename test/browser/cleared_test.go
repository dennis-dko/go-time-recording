//go:build browser

package browser

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// Clearing an optional field and saving has to clear it.
//
// The server is built for this and says so where it reads the update: an end date
// "present but empty is the request to take it off. Absent still means 'leave it
// alone', which is what a partial update is for, so the two need telling apart -
// and an empty string is how a form field that has been cleared arrives."
// UpdateTimesheetCommand.Description is the same three states, and the service
// applies a non-nil empty string by clearing the description.
//
// It never arrives. formData builds the payload with
// `if (value !== ”) out[key] = value`, so an emptied box is dropped rather than
// sent as "", the field is absent, and absent means leave it alone. The entry
// keeps the old description and the screen says "Entry saved".
//
// The project form already worked around exactly this, and its comment explains
// the mechanism: "Written out rather than read with formData, which drops an
// empty value - and an emptied optional field is the whole point here." The
// booking form is the sibling that was not done - the same shape as the three
// finds before it in this rotation, and found by asking the question those made a
// rule.
func TestClearingADescriptionActuallyClearsIt(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.run("open the entries view", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID))

	// An entry with something in the box that has to be removable.
	p.run("book one with a description",
		chromedp.SendKeys(`#form-timesheet input[name="durationHours"]`, "1.25", chromedp.ByQuery),
		chromedp.SendKeys(`#form-timesheet input[name="description"]`, "first draft",
			chromedp.ByQuery),
		p.click(`#form-timesheet button[type="submit"]`),
		chromedp.WaitVisible(`#table-timesheets tbody tr`, chromedp.ByQuery))

	p.run("open it for correction",
		p.click(`#table-timesheets tbody tr:first-child .actions button.link`),
		chromedp.WaitVisible(`#timesheet-cancel`, chromedp.ByQuery))

	var loaded string

	p.run("the box holds what was booked", chromedp.Evaluate(
		`document.querySelector('#form-timesheet input[name="description"]').value`, &loaded))

	if loaded != "first draft" {
		t.Fatalf("the form opened holding %q, so this case is not editing the entry "+
			"it booked", loaded)
	}

	p.run("clear it and save",
		chromedp.Evaluate(`(() => {
			const box = document.querySelector('#form-timesheet input[name="description"]');
			box.value = '';
			box.dispatchEvent(new Event('input', { bubbles: true }));
			return true;
		})()`, nil),
		p.click(`#form-timesheet button[type="submit"]`),
		chromedp.WaitNotPresent(`#timesheet-cancel:not([hidden])`, chromedp.ByQuery))

	// Read back from the server rather than from the row, so this cannot pass on
	// a screen that merely looks right.
	var stored string

	p.run("ask the server what it kept", chromedp.Evaluate(
		`(async () => {
			const res = await fetch('/api/v1/timesheets', { credentials: 'same-origin' });
			const body = await res.json();
			const items = body.data.items ?? [];
			return items.length ? (items[0].description ?? '') : '(no entry)';
		})()`, &stored, awaitPromise))

	if stored != "" {
		t.Errorf("the description was cleared and saved, and the server still holds "+
			"%q. An emptied optional field is dropped by formData, so it never "+
			"arrives, and absent means leave it alone", stored)
	}
}

// And the same for the project an entry is booked against.
//
// The same form, the same cause, one field along. The service says how a client
// asks for this - "0 is how a client asks to remove the assignment again, which
// is what makes an entry uncategorised" - and the form sends
// `projectId: Number(raw.projectId)`. Number(”) is 0, which would be exactly
// right; but formData has already dropped the empty value, so what is read is
// Number(undefined), which is NaN, which JSON.stringify writes as null, which the
// server reads as "leave it alone".
//
// Its own case because it is its own claim. The description proves formData drops
// the field; this proves the entry cannot be uncategorised again, which is a
// different thing to be unable to do.
func TestUnassigningAProjectActuallyUnassignsIt(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	// A project of this account's own, made through the API so the case stays
	// about the booking form.
	p.run("create a project", chromedp.Evaluate(`(async () => {
		const token = document.cookie.split('; ')
			.find((c) => c.startsWith('gtr_csrf=')).split('=')[1];
		const res = await fetch('/api/v1/projects', {
			method: 'POST',
			credentials: 'same-origin',
			headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': token },
			body: JSON.stringify({ name: 'Roof', startDate: '2026-08-03' }),
		});
		return res.status;
	})()`, nil, awaitPromise))

	// Reloaded, because the select was filled before the project existed and
	// nothing re-fills it for a change made behind the screen's back.
	p.run("pick the new project up",
		chromedp.Reload(),
		chromedp.WaitVisible(`html[data-loaded="yes"]`, chromedp.ByQuery))

	p.run("open the entries view", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID))

	p.run("book one against it", chromedp.Evaluate(`(() => {
		const form = document.querySelector('#form-timesheet');
		const project = form.elements.projectId;
		const option = [...project.options].find((o) => o.textContent.includes('Roof'));
		project.value = option.value;
		project.dispatchEvent(new Event('change', { bubbles: true }));
		form.elements.durationHours.value = '2';
		form.elements.durationHours.dispatchEvent(new Event('input', { bubbles: true }));
		return true;
	})()`, nil),
		p.click(`#form-timesheet button[type="submit"]`),
		chromedp.WaitVisible(`#table-timesheets tbody tr`, chromedp.ByQuery))

	p.run("open it and take the project off",
		p.click(`#table-timesheets tbody tr:first-child .actions button.link`),
		chromedp.WaitVisible(`#timesheet-cancel`, chromedp.ByQuery),
		chromedp.Evaluate(`(() => {
			const project = document.querySelector('#form-timesheet').elements.projectId;
			project.value = '';
			project.dispatchEvent(new Event('change', { bubbles: true }));
			return true;
		})()`, nil),
		p.click(`#form-timesheet button[type="submit"]`),
		chromedp.WaitNotPresent(`#timesheet-cancel:not([hidden])`, chromedp.ByQuery))

	var stored any

	p.run("ask the server what it kept", chromedp.Evaluate(`(async () => {
		const res = await fetch('/api/v1/timesheets', { credentials: 'same-origin' });
		const body = await res.json();
		const items = body.data.items ?? [];
		return items.length ? (items[0].projectId ?? null) : 'no entry';
	})()`, &stored, awaitPromise))

	if stored != nil {
		t.Errorf("the project was taken off and saved, and the server still has the "+
			"entry on project %v. An emptied select is dropped by formData, so what "+
			"is sent is Number(undefined) - NaN, then null - and null is not the 0 "+
			"the service reads as \"remove the assignment\"", stored)
	}
}
