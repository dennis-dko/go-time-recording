//go:build browser

package browser

import (
	"encoding/json"
	"testing"

	"github.com/chromedp/chromedp"
)

// A form correcting an entry goes on saying so after the language changes.
//
// Creating and correcting share one form, told apart by a hidden id, and a form
// in edit mode swaps its title and its submit label to say which job it is doing.
// Both are set with textContent from the script - while the elements keep the
// data-i18n they were written with in the markup, which names the *creating* key:
// ts.book on the heading and action.book on the button.
//
// applyLanguage translates every [data-i18n] node from its key. So a language
// change while a correction is open puts the heading back to "Book time" and the
// button back to "Book", and nothing puts the hidden id back - the form still
// carries the entry it opened. The screen then offers to book a new entry and
// updates an existing one when pressed.
//
// The id is what this checks alongside the words: a heading that disagrees with
// the form is a display fault, and a heading that disagrees with what the button
// will do is not.
func TestACorrectionStillSaysSoAfterALanguageChange(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.run("book an entry", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID),
		chromedp.SendKeys(`#form-timesheet input[name="durationHours"]`, "1.25",
			chromedp.ByQuery),
		p.click(`#form-timesheet button[type="submit"]`),
		chromedp.WaitVisible(`#table-timesheets tbody tr`, chromedp.ByQuery))

	p.run("open it for correction",
		p.click(`#table-timesheets tbody tr:first-child .actions button.link`),
		chromedp.WaitVisible(`#timesheet-cancel`, chromedp.ByQuery))

	read := func(when string) (title, submit, id string) {
		t.Helper()

		var out string

		p.run("read the form "+when, chromedp.Evaluate(`JSON.stringify({
			title: document.querySelector('#timesheet-form-title').textContent.trim(),
			submit: document.querySelector('#timesheet-submit').textContent.trim(),
			id: document.querySelector('#form-timesheet').elements.id.value,
		})`, &out))

		var form struct {
			Title  string `json:"title"`
			Submit string `json:"submit"`
			ID     string `json:"id"`
		}

		if err := json.Unmarshal([]byte(out), &form); err != nil {
			t.Fatalf("reading the form (%q): %v", out, err)
		}

		return form.Title, form.Submit, form.ID
	}

	title, submit, id := read("while correcting")

	if id == "" {
		t.Fatal("the form is not holding an entry, so this case is not about a " +
			"correction at all")
	}

	p.chooseLanguage("de")

	germanTitle, germanSubmit, stillEditing := read("after switching to German")

	if stillEditing != id {
		t.Fatalf("the language change also left the form (id %q became %q); this "+
			"case is about the labels while the correction is still open",
			id, stillEditing)
	}

	// And back to English, which is the half applyLanguage restores from its own
	// copy of the source. That copy is taken the first time an element is
	// translated, so one taken while the creating key was declared comes back as
	// the creating message however right the German looked.
	p.chooseLanguage("en")

	backTitle, backSubmit, _ := read("back in English")

	if backTitle != title || backSubmit != submit {
		t.Errorf("after German and back the form reads %q / %q rather than %q / %q, "+
			"while still holding entry %q", backTitle, backSubmit, title, submit, id)
	}

	// Compared against what the same form says when it is creating, in the same
	// language - which is the state the labels must not have gone back to.
	p.run("cancel the correction", p.click("#timesheet-cancel"))

	creatingTitle, creatingSubmit, _ := read("while creating")

	p.chooseLanguage("de")

	germanCreatingTitle, germanCreatingSubmit, _ := read("while creating, in German")

	if germanTitle == germanCreatingTitle {
		t.Errorf("after the language change the heading reads %q, which is what it "+
			"says when creating - while the form was still holding entry %q",
			germanTitle, id)
	}

	if germanSubmit == germanCreatingSubmit {
		t.Errorf("after the language change the button reads %q, which is what it "+
			"says when creating - and pressing it would have updated entry %q",
			germanSubmit, id)
	}

	if t.Failed() {
		t.Logf("before the change: %q / %q; after: %q / %q; creating: %q / %q",
			title, submit, germanTitle, germanSubmit, creatingTitle, creatingSubmit)
	}
}

// And the order the source copy exists for: German first, then the correction.
//
// applyLanguage takes its copy of the English source the first time it
// translates an element - so on a page already read in German the copy is
// "Book time", taken while the creating key was the declared one. Opening a
// correction after that and going back to English restored that copy: the
// heading read "Book time" over a form holding an entry, in English, having
// looked right in German the whole time.
//
// This is why swapTheLabel writes the source as well as the key. The case above
// starts in English and cannot tell the two apart, because there the copy is
// taken from text the script had already corrected.
func TestACorrectionOpenedInGermanSaysSoInEnglish(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.run("book an entry", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID),
		chromedp.SendKeys(`#form-timesheet input[name="durationHours"]`, "2.5",
			chromedp.ByQuery),
		p.click(`#form-timesheet button[type="submit"]`),
		chromedp.WaitVisible(`#table-timesheets tbody tr`, chromedp.ByQuery))

	// German before the correction, so the source copy is taken while the form is
	// still declaring itself as the one that creates.
	p.chooseLanguage("de")

	p.run("open it for correction",
		p.click(`#table-timesheets tbody tr:first-child .actions button.link`),
		chromedp.WaitVisible(`#timesheet-cancel`, chromedp.ByQuery))

	p.chooseLanguage("en")

	var out string

	p.run("read the form in English", chromedp.Evaluate(`JSON.stringify({
		title: document.querySelector('#timesheet-form-title').textContent.trim(),
		submit: document.querySelector('#timesheet-submit').textContent.trim(),
		id: document.querySelector('#form-timesheet').elements.id.value,
	})`, &out))

	var form struct {
		Title  string `json:"title"`
		Submit string `json:"submit"`
		ID     string `json:"id"`
	}

	if err := json.Unmarshal([]byte(out), &form); err != nil {
		t.Fatalf("reading the form (%q): %v", out, err)
	}

	if form.ID == "" {
		t.Fatal("the form is not holding an entry, so this case is not about a " +
			"correction")
	}

	if form.Title != "Edit entry" || form.Submit != "Save" {
		t.Errorf("back in English the form reads %q / %q rather than "+
			"\"Edit entry\" / \"Save\", while holding entry %q. The copy of the "+
			"English source was taken while the creating key was declared",
			form.Title, form.Submit, form.ID)
	}
}
