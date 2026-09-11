package web_test

import (
	"strings"
	"testing"
)

// The small "delete" in a table row is a text button, not a filled one.
//
// button.link sets no background and the solid danger rule sets a red one at the
// same specificity, so source order decided it - and the solid rule came second.
// Every row's delete button turned into a red rectangle with red text in it: a
// coloured block with an invisible label, which is what a screenshot showed.
func TestTheSolidDangerButtonSparesTextButtons(t *testing.T) {
	css := asset(t, "/app.css")

	if !strings.Contains(css, "button.danger:not(.link)") {
		t.Error("the solid danger rule applies to .link buttons too, which paints the " +
			"row actions red on red")
	}

	// And the text-button rule is still there to colour them.
	if !strings.Contains(css, "button.link.danger") {
		t.Error("nothing colours the text of a destructive row action")
	}
}

// Selecting several rows to delete is derived from the rows themselves: the
// checkbox appears where a delete button already does, so the two cannot come to
// disagree about who may delete what.
//
// That only holds while every table asks deleteButton() for its delete button. A
// hand-rolled one looks identical on screen and is invisible to the column, so
// that table would quietly be the one without bulk deletion - and nobody would
// notice until they went looking for it.
func TestEveryRowDeletionGoesThroughTheSharedButton(t *testing.T) {
	js := asset(t, "/app.js")

	const built = "class: 'link danger'"

	if got := strings.Count(js, built); got != 1 {
		t.Errorf("%d places build a destructive row button by hand, want 1 (deleteButton); "+
			"a table that builds its own gets no checkbox column, because the column is "+
			"derived from the buttons", got)
	}

	// The derivation itself, in both directions: the button records what it would
	// delete, and the column reads it back.
	for _, needed := range []string{"button.deletes = { label, path, message, after }",
		"row.querySelectorAll('button.danger')", "box.deletes = deletes"} {
		if !strings.Contains(js, needed) {
			t.Errorf("app.js no longer contains %q, so the checkbox column and the delete "+
				"buttons are no longer the same decision", needed)
		}
	}
}
