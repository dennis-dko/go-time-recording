//go:build browser

package browser

import (
	"strconv"
	"testing"

	"github.com/chromedp/chromedp"
)

// A booking is kept as a template from the form, and choosing the template fills
// the form again - project, hours and description - without booking anything and
// without touching the date. Taken back from its card, it leaves the screen.
func TestATemplateFillsTheBookingForm(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	var projectID int

	p.awaitJSON(`(async () => {
		const made = await api('/projects', {
			method: 'POST', body: JSON.stringify({ name: 'Internal tools', startDate: '2026-01-01' }),
		});
		await loadProjects();
		return made.id;
	})()`, &projectID)

	project := strconv.Itoa(projectID)

	p.run("open the entries", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID))

	p.waitForNode(`#form-timesheet select[name=projectId] option[value="` + project + `"]`)

	p.run("fill the form in", chromedp.Evaluate(`(() => {
		const form = document.querySelector('#form-timesheet');
		form.elements.projectId.value = '`+project+`';
		form.elements.durationHours.value = '0.25';
		form.elements.description.value = 'Daily';
	})()`, nil))

	p.run("keep it as a template", p.click("#ts-template-save"))

	p.waitShown("#ts-template-row")
	p.waitShown("#templates-card")

	var date string

	p.run("empty the form and choose the template", chromedp.Evaluate(`(() => {
		resetTimesheetForm();
		const form = document.querySelector('#form-timesheet');
		form.elements.projectId.value = '';
		const picker = document.querySelector('#ts-template');
		picker.value = picker.options[1].value;
		picker.dispatchEvent(new Event('change', { bubbles: true }));
	})()`, nil),
		chromedp.Evaluate(`document.querySelector('#form-timesheet').elements.date.value`, &date))

	var filled struct {
		Project, Hours, Description, Picker string
		Booked                              int
	}

	p.awaitJSON(`(async () => {
		const form = document.querySelector('#form-timesheet');
		const booked = (await api('/timesheets'))?.items?.length ?? -1;
		return {
			Project: form.elements.projectId.value,
			Hours: form.elements.durationHours.value,
			Description: form.elements.description.value,
			Picker: document.querySelector('#ts-template').value,
			Booked: booked,
		};
	})()`, &filled)

	if filled.Project != project || filled.Hours != "0.25" || filled.Description != "Daily" {
		t.Errorf("the form reads %+v after choosing the template, want project %s, 0.25 and Daily", filled, project)
	}

	if filled.Booked != 0 {
		t.Errorf("%d entries exist after choosing a template, want none - it only fills the form", filled.Booked)
	}

	if filled.Picker != "" {
		t.Errorf("the picker still shows %q after filling the form, want it back at its placeholder", filled.Picker)
	}

	if date == "" {
		t.Error("the date was emptied by the template, want it left as it was")
	}

	p.run("take the template back", p.click("#table-templates button.danger"),
		chromedp.WaitVisible(".confirm-card", chromedp.ByQuery),
		p.click(".confirm-card button.confirm-proceed"))

	p.waitGone("#templates-card:not([hidden])")

	if p.visible("#ts-template-row") {
		t.Error("the picker is still on the form with no template left to choose")
	}
}

// A template whose project has closed since stays listed, but cannot be chosen:
// filling the form with it would put a project into the select that the select
// does not offer.
func TestATemplateOnAClosedProjectCannotBeChosen(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	var disabled bool

	p.awaitJSON(`(async () => {
		const made = await api('/projects', {
			method: 'POST', body: JSON.stringify({ name: 'Finished work', startDate: '2026-01-01' }),
		});
		await api('/me/templates', { method: 'POST', body: JSON.stringify({ projectId: made.id, durationHours: 1 }) });
		await api('/projects/' + made.id, { method: 'PUT', body: JSON.stringify({ status: 'completed' }) });
		await loadProjects();
		await loadTemplates();
		return true;
	})()`, nil)

	p.waitShown("#ts-template-row")

	p.awaitJSON(`document.querySelector('#ts-template').options[1].disabled`, &disabled)

	if !disabled {
		t.Error("a template on a finished project can be chosen, want it offered but closed")
	}
}

// A sign-out takes the templates with it: they are somebody's own bookings, and
// the picker and the card are on the page whoever signs in next.
func TestASignOutTakesTheTemplatesWithIt(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.awaitJSON(`(async () => {
		await api('/me/templates', { method: 'POST', body: JSON.stringify({ description: 'A private routine' }) });
		await loadTemplates();
		return true;
	})()`, nil)

	p.waitShown("#templates-card")

	p.run("sign out", p.click("#logout"), chromedp.WaitVisible("#form-login", chromedp.ByID))

	var left []string

	p.awaitJSON(`(() => {
		const left = [];
		if (document.querySelector('#ts-template').options.length) left.push('the picker still offers templates');
		if (document.querySelector('#table-templates tbody').childElementCount) left.push('the card still lists templates');
		if (!document.querySelector('#templates-card').hidden) left.push('the card is still shown');
		if (!document.querySelector('#ts-template-row').hidden) left.push('the picker is still shown');
		return left;
	})()`, &left)

	for _, what := range left {
		t.Errorf("after the sign-out %s", what)
	}
}
