//go:build browser

package browser

import (
	"slices"
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

	p.chooseLanguage("de")

	p.waitForText(`.tab[data-view="timesheets"]`, "Zeiteinträge")

	var protected bool

	// Filled the way a person fills it, and that is not a detail.
	//
	// This card is refilled from the server by loaders that run for reasons
	// having nothing to do with whoever is at the keyboard - the language switch
	// two lines above is one - and what stops a late one landing on top of typed
	// values is the form being marked as being edited. What marks it is an input
	// or a change event reaching the *form*, which is where watchForEditing
	// listens.
	//
	// Neither happened here. Setting a value from script fires nothing at all,
	// which is exactly the distinction that mechanism rests on, and the change
	// dispatched on the select did not bubble, so it reached the select's own
	// listener and never the form. So the form was unprotected, the refill from
	// the language switch could arrive after these lines, and the connection
	// tested was the instance's own working one - the case failed reporting "Die
	// Verbindung funktioniert."
	//
	// Only ever under load, because the refill has to be slow enough to arrive
	// late. It went green here and red on a busy runner, which is the least
	// useful way for a case to be wrong.
	p.run("point it at nothing", chromedp.Evaluate(`
		(() => {
			const form = document.querySelector('#form-datasource');
			const typed = (field, value) => {
				field.value = value;
				field.dispatchEvent(new Event('input', { bubbles: true }));
			};

			// The type first, and with its own change: the fields below depend on
			// it, and picking one prefills the port.
			//
			// And marked as chosen, which is a separate thing from being typed into
			// and is why this case used to lose its race. showRunningConnection puts
			// the running connection's type back over whatever the form holds -
			// deliberately, and outside the guard that protects the text fields,
			// because the type decides which fields exist at all and a form counted
			// as edited for any reason left the card describing a SQLite file under
			// a line reading "connected via postgres".
			//
			// What stops it is somebody actually picking one, and isTrusted is how
			// the application tells a person from a script - restoring a draft sets
			// a value and dispatches a change exactly as these lines do. Nothing
			// dispatched from here can be trusted, so this leaves the mark a real
			// choice leaves rather than pretending to be one.
			form.elements.dialect.value = 'postgres';
			form.elements.dialect.dataset.chosen = 'yes';
			form.elements.dialect.dispatchEvent(new Event('change', { bubbles: true }));

			// A host nobody is listening on. Postgres rather than the file the
			// instance actually runs on, so the failure is a refused connection
			// rather than a complaint about a field.
			typed(form.elements.host, '127.0.0.1');
			typed(form.elements.port, '1');
			typed(form.elements.name, 'gtr');
			typed(form.elements.user, 'gtr');
			typed(form.elements.password, 'gtr');

			return form.dataset.editing === 'yes';
		})()`, &protected))

	// The refill, forced rather than waited for.
	//
	// This is the race the case used to lose on a busy runner and win on a quiet
	// one: choosing a language reloads every screen, and if that reload lands
	// after the typing above it decides what is in these boxes. Running it here
	// makes the outcome the same on every machine - and if the boxes cannot
	// survive a reload, this case should say so rather than depend on which
	// machine it is on.
	p.run("reload the card underneath it", chromedp.Evaluate(
		`loadAdmin()`, nil, awaitPromise))

	var still string

	p.run("what the type says now", chromedp.Evaluate(
		`document.querySelector('#form-datasource').elements.dialect.value`, &still))

	if still != "postgres" {
		t.Fatalf("after a reload the type is %q rather than \"postgres\", so the "+
			"connection about to be tested is the instance's own working one and "+
			"the case would report \"Die Verbindung funktioniert.\"", still)
	}

	if !protected {
		t.Fatal("the form was not marked as being edited, so a loader may still put " +
			"the stored connection back over what was just typed")
	}

	// The box is watched from before the press rather than polled after it. The
	// line that stands there while the attempt runs is gone the instant the
	// outcome arrives, and a connection refused by a local port arrives in about a
	// millisecond - so a poll looking for that line finds it on a loaded runner
	// and misses it on a fast one, which is exactly the wrong way round.
	//
	// Recording every state the box passes through catches it either way, and
	// still tells the next failure which half went wrong: nothing recorded at all
	// means the press never landed.
	p.run("watch the box", chromedp.Evaluate(`
		(() => {
			window.__states = [];

			const box = document.querySelector('#datasource-test-result');

			new MutationObserver(() => window.__states.push(box.textContent.slice(0, 40)))
				.observe(box, { childList: true, subtree: true, characterData: true });

			return 1;
		})()`, nil))

	p.run("test the connection", p.click("#datasource-test"))

	// The generic sentence, in German. Waited for on the outcome rather than on
	// the word "Verbindung", which is also in "Verbindung wird geprüft …".
	//
	// Given longer than the usual wait: the attempt is a connection to a port
	// nobody answers on, and how long that takes to fail is the network stack's
	// business rather than this application's.
	p.waitForTextWithin("#datasource-test-result", "konnte nicht", 40*time.Second)

	// And the press did land: the running line stood there at some point between
	// the click and the outcome. Read from the recording, because by the time
	// anything can be asked about the box the outcome has replaced it.
	var states []string

	p.run("read what the box said", chromedp.Evaluate(`window.__states`, &states))

	if !slices.ContainsFunc(states, func(said string) bool {
		return strings.Contains(said, "wird geprüft")
	}) {
		t.Errorf("the box never said the check was running; it said: %q", states)
	}

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
