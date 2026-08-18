//go:build browser

package browser

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// A connection that will not open says so in the reader's language, and keeps
// what the driver said one click away.
//
// This is the case the whole arrangement exists for. The failure comes back as
// "dial tcp 10.0.0.4:5432: connect: connection refused" or "password
// authentication failed for user" - written by a driver, in English, from a set
// of sentences with no end. Nothing here can translate it, and it used to be the
// entire message: on a German screen, on the one card where the values being
// complained about are in the fields directly above, the only line offered was
// the one line that reader could not use.
//
// So the sentence is generic and translated, and the driver's own words are
// folded away underneath for whoever is going to act on them - which on this
// screen is often the same person, a moment later.
func TestAFailedConnectionSaysSoInGermanAndKeepsTheDetail(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-datasource", chromedp.ByID))

	// Waited for the card to be filled before anything is typed into it: the form
	// is in index.html, so it is on screen before a single request has answered,
	// and the answer overwrites every field when it arrives.
	p.waitForFilled("#datasource-active")

	p.run("switch to German",
		chromedp.SetValue("#language-picker", "de", chromedp.ByID),
		chromedp.Evaluate(
			`document.querySelector('#language-picker').dispatchEvent(new Event('change'))`, nil))

	p.waitForText(`.tab[data-view="timesheets"]`, "Zeiteinträge")

	// A host nobody is listening on. Postgres rather than the file the instance
	// actually runs on, so the failure is a refused connection rather than a
	// complaint about a field.
	p.run("point it at nothing",
		chromedp.SetValue(`#form-datasource select[name="dialect"]`, "postgres",
			chromedp.ByQuery),
		chromedp.Evaluate(
			`document.querySelector('#form-datasource select[name="dialect"]')
				.dispatchEvent(new Event('change'))`, nil))

	for field, value := range map[string]string{
		"host": "127.0.0.1", "port": "1", "name": "gtr", "user": "gtr", "password": "gtr",
	} {
		p.run("fill "+field, chromedp.SetValue(
			`#form-datasource [name="`+field+`"]`, value, chromedp.ByQuery))
	}

	p.run("test the connection", p.click("#datasource-test"))

	// Two waits rather than one, so a failure says which half went wrong.
	//
	// The first is the line that stands there while the attempt runs, which only
	// appears if the press landed at all. Waiting straight for the outcome
	// reported an empty box, which is equally what a missed click and a slow
	// connection look like - and on a loaded runner it was one of those.
	p.waitForText("#datasource-test-result", "wird geprüft")

	// The generic sentence, in German. Waited for on the outcome rather than on
	// the word "Verbindung", which is also in "Verbindung wird geprüft …".
	//
	// Given longer than the usual wait: the attempt is a connection to a port
	// nobody answers on, and how long that takes to fail is the network stack's
	// business rather than this application's.
	p.waitForTextWithin("#datasource-test-result", "konnte nicht", 40*time.Second)

	// And whatever the outcome, the box says something. A refusal with no words
	// in it used to render as an empty box - which is the one outcome that says
	// less than not having pressed the button, and what a run of this on a loaded
	// machine actually reported.
	if said := strings.TrimSpace(p.text("#datasource-test-result")); said == "" {
		t.Error("the connection test finished and said nothing at all")
	}

	// The sentence that leads, read on its own. The whole card's text includes
	// the folded-away part - textContent does not care what is on screen - so
	// asserting against that would be asserting against both at once, which is
	// the thing being separated here.
	var leading string

	p.run("read the sentence", chromedp.Evaluate(
		`document.querySelector('#datasource-test-result > span')?.textContent ?? ''`,
		&leading))

	if leading == "" {
		t.Fatal("the failure has no sentence of its own, only whatever was attached")
	}

	if strings.Contains(leading, "dial tcp") || strings.Contains(leading, "connectex") {
		t.Errorf("the driver's own English is what the screen leads with: %q", leading)
	}

	if !strings.Contains(leading, "konnte nicht hergestellt werden") {
		t.Errorf("the sentence is not the German one: %q", leading)
	}

	// The detail is there and is folded away rather than absent: the words that
	// say what actually happened are the ones an administrator needs.
	if p.count("#datasource-test-result details.refusal-detail") != 1 {
		t.Fatal("the failure offers nothing to expand, so what the driver said is gone")
	}

	// Closed to begin with. Open, it would put a paragraph of somebody else's
	// English on the screen for every reader who cannot use it.
	var open bool

	p.run("is it open", chromedp.Evaluate(
		`document.querySelector('#datasource-test-result details').open`, &open))

	if open {
		t.Error("the technical detail is unfolded by default")
	}

	// And unfolding it gives the driver's own words back.
	p.run("unfold it", p.click("#datasource-test-result details summary"))

	detail := p.text("#datasource-test-result details")

	if !strings.Contains(detail, "127.0.0.1") && !strings.Contains(detail, "connect") {
		t.Errorf("the expanded detail does not say what actually failed: %q", detail)
	}
}
