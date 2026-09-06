//go:build browser

package browser

import (
	"encoding/json"
	"testing"

	"github.com/chromedp/chromedp"
)

// Every operational field says what leaving it empty will do.
//
// That is the whole arrangement, and the response type says so: "Defaults is
// what the environment supplies, shown as the placeholder in each empty field",
// carried "so the screen can show what a blank field means instead of leaving
// the reader to guess."
//
// Five of the six do. OperationalLimits - which is both the effective values and
// the defaults on the wire - has no sessionIdleMinutes, although model.Limits
// does and the form has a box for it. So the idle timeout is the one field that
// leaves the reader to guess, and it is also missing from the "Currently in
// force" line above, where the other five are named.
//
// It is not a minor field to be quiet about: it ends a session that has gone
// unused, whatever is left of its lifetime, and zero switches it off.
func TestEveryOperationalFieldSaysWhatEmptyMeans(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open the card", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-operational", chromedp.ByID))

	var out string

	p.run("read every placeholder", chromedp.Evaluate(`JSON.stringify(
		Object.fromEntries([...document.querySelectorAll('#form-operational input')]
			.filter((input) => input.name)
			.map((input) => [input.name, input.placeholder])))`, &out))

	var shown map[string]string

	if err := json.Unmarshal([]byte(out), &shown); err != nil {
		t.Fatalf("reading the placeholders (%q): %v", out, err)
	}

	if len(shown) < 6 {
		t.Fatalf("found %d fields on the card, which is fewer than the six it has: %v",
			len(shown), shown)
	}

	for name, placeholder := range shown {
		if placeholder != "" {
			continue
		}

		t.Errorf("%s has no placeholder, so leaving it empty says nothing about "+
			"what will apply. The other fields carry the value the environment "+
			"supplies", name)
	}

	var inForce string

	p.run("read what is in force", chromedp.Evaluate(
		`document.querySelector('#operational-effective').textContent.trim()`, &inForce))

	t.Logf("in force: %s", inForce)
}
