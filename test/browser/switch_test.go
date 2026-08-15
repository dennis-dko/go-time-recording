//go:build browser

package browser

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// Everything that turns something on or off is drawn as a switch.
//
// A tick box and a switch say different things. A tick box says "include this
// one": it belongs in a list where several are chosen, and choosing does nothing
// until the form is submitted. A switch says "this is on" - one thing, two
// states, and the state is the state of the installation rather than a selection
// waiting to be applied. Maintenance mode is the clearest case: a box beside the
// words "out of service" reads as an item in a list of settings, and it is not.
//
// So the rule is structural rather than a list of ids, and it is checked as one:
// a checkbox is either a switch or a selection, and a selection is a row being
// picked or a permission being granted. Anything else that appears is a new
// control nobody has decided about, which is exactly when the decision should be
// made rather than a year later.
func TestEveryOnOffControlIsASwitch(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	views := []string{"users", "roles", "settings", "admin"}

	for _, view := range views {
		p.run("open "+view, p.click(`.tab[data-view="`+view+`"]`))
		p.settleWelcome()

		var stray []string

		p.evalJSON(`JSON.stringify([...document.querySelectorAll('input[type="checkbox"]')]
			.filter(box => box.offsetParent !== null)
			.filter(box => !box.closest('label.switch'))
			// Selections, which are deliberately not switches. The line between
			// them is what the control means rather than what it does to a
			// boolean: a switch is one thing with two states and the state is the
			// installation's, while these are all "which of these do I want" -
			// several chosen from a list, in a grid where a row of switches would
			// be both heavier to read and slower to scan.
			//
			// Rows being picked for deletion, permissions being granted on a role,
			// and the levels the log viewer shows.
			.filter(box => !box.classList.contains('row-pick'))
			.filter(box => !box.classList.contains('pick-all'))
			.filter(box => !box.closest('.perm-grid'))
			.filter(box => !box.closest('#log-levels'))
			.map(box => (box.id || box.name || box.className || 'unnamed')
				+ ' in <' + (box.closest('[id]')?.id ?? '?') + '> near "'
				+ (box.closest('label')?.textContent ?? '').trim().slice(0, 40) + '"'))`, &stray)

		for _, name := range stray {
			t.Errorf("on %s, the checkbox %q switches something on or off and is "+
				"still drawn as a tick box", view, name)
		}
	}
}

// A switch switches, and does so without a line of script.
//
// The visible part is a span; the control is a checkbox underneath it, made
// invisible and left exactly where the span is drawn. That arrangement is easy to
// get subtly wrong - an input moved out from under its track, a label association
// broken by a wrapper - and the failure is a control that looks perfect and does
// nothing.
func TestPressingASwitchChangesTheStateUnderneath(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-maintenance", chromedp.ByID))

	before := p.switchIsOn(`#form-maintenance input[name="enabled"]`)

	// Pressed on the part somebody can see, not on the control.
	p.run("press the switch", p.click(`#form-maintenance label.switch .switch-track`))

	if p.switchIsOn(`#form-maintenance input[name="enabled"]`) == before {
		t.Fatal("pressing the switch did not change what it is switching")
	}

	// And the keyboard reaches it, which is the half that quietly disappears when
	// a native control is replaced by something drawn.
	p.run("focus and press space",
		chromedp.Focus(`#form-maintenance input[name="enabled"]`, chromedp.ByQuery),
		chromedp.KeyEvent(" "))

	if p.switchIsOn(`#form-maintenance input[name="enabled"]`) != before {
		t.Error("the space bar does not work the switch, so it cannot be used " +
			"without a pointer")
	}
}

// switchIsOn reads the state of the control under a switch.
func (p *page) switchIsOn(selector string) bool {
	p.t.Helper()

	var on bool

	p.run("read the switch", chromedp.Evaluate(
		`document.querySelector('`+selector+`').checked`, &on))

	return on
}
