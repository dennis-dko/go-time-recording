//go:build browser

package browser

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// The placeholder mark is the size of a logo, and does not behave like text.
//
// It stands in the slot an uploaded logo takes, and an uploaded logo is up to
// 96 high - so a 26-pixel chip in that space read as a favicon that had
// wandered into the bar rather than as the installation's own sign.
//
// And it is drawn with an SVG <text>, which is a letter as far as the browser
// is concerned: the I-beam cursor over it, selectable, draggable, copyable. A
// mark that can be selected announces itself as something somebody typed.
func TestThePlaceholderMarkIsALogoRatherThanALetter(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	var drawn struct {
		Height   float64 `json:"height"`
		Width    float64 `json:"width"`
		Cursor   string  `json:"cursor"`
		Select   string  `json:"select"`
		Pointer  string  `json:"pointer"`
		Letter   string  `json:"letter"`
		Selected string  `json:"selected"`
	}

	p.evalJSON(`(() => {
		const mark = document.querySelector('#brand-mark');
		const letter = document.querySelector('#brand-initial');
		const style = window.getComputedStyle(letter);
		const box = mark.getBoundingClientRect();

		// What a drag across the mark would take with it.
		const range = document.createRange();
		range.selectNodeContents(letter);

		return JSON.stringify({
			height: box.height,
			width: box.width,
			cursor: style.cursor,
			select: style.userSelect,
			pointer: style.pointerEvents,
			letter: letter.textContent,
			selected: range.toString(),
		});
	})()`, &drawn)

	if drawn.Letter == "" {
		t.Fatal("the placeholder carries no initial, so there is nothing to measure")
	}

	// Three times what it was, which is the size the slot is for.
	if drawn.Height < 70 || drawn.Width < 70 {
		t.Errorf("the placeholder is %.0fx%.0f, which is a favicon rather than a logo",
			drawn.Width, drawn.Height)
	}

	if drawn.Cursor != "default" {
		t.Errorf("the mark takes the %q cursor, so it reads as text", drawn.Cursor)
	}

	if drawn.Select != "none" {
		t.Errorf("the letter can be selected (user-select: %q)", drawn.Select)
	}

	if drawn.Pointer != "none" {
		t.Errorf("the letter takes the pointer (pointer-events: %q), so it can be "+
			"dragged over", drawn.Pointer)
	}
}

// The restart card comes with a reason for it, and goes when there is none.
//
// It was on screen whenever somebody could administer, so that a restart could
// be asked for on purpose. What that produced was a card offering a restart
// beside the version card offering an update - and the update button restarts by
// itself, so the two read as two ways to do one thing, one of them wrong.
func TestTheRestartCardIsOnlyThereWhenSomethingNeedsIt(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-telemetry", chromedp.ByID))

	p.waitForFilled("#tel-tracing-hint")

	if p.visible("#restart-card") {
		t.Error("the restart card is offered on an installation with nothing " +
			"waiting for one, beside the version card that restarts by itself")
	}

	// Something only the next start reads.
	p.run("ask for tracing",
		chromedp.SetValue(`#form-telemetry [name="traceExporter"]`, "otlp", chromedp.ByQuery),
		chromedp.Evaluate(
			`document.querySelector('#form-telemetry [name="traceExporter"]')
				.dispatchEvent(new Event('change', { bubbles: true }))`, nil),
		chromedp.SetValue(`#form-telemetry [name="tracerUrl"]`, "jaeger:4317", chromedp.ByQuery),
		p.click(`#form-telemetry button[type="submit"]`))

	p.waitShown("#restart-card")

	if !p.visible("#restart-card") {
		t.Fatal("nothing offers a restart although a setting is waiting for one")
	}

	if said := p.text("#restart-card-pending"); said == "" {
		t.Error("the card does not say what is waiting")
	}
}

// The notice reads as one sentence, with the way to the card inside it.
func TestTheRestartNoticeCarriesItsLinkInsideTheSentence(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-telemetry", chromedp.ByID))

	p.waitForFilled("#tel-tracing-hint")

	p.run("ask for tracing",
		chromedp.SetValue(`#form-telemetry [name="traceExporter"]`, "otlp", chromedp.ByQuery),
		chromedp.Evaluate(
			`document.querySelector('#form-telemetry [name="traceExporter"]')
				.dispatchEvent(new Event('change', { bubbles: true }))`, nil),
		chromedp.SetValue(`#form-telemetry [name="tracerUrl"]`, "jaeger:4317", chromedp.ByQuery),
		p.click(`#form-telemetry button[type="submit"]`))

	p.waitShown("#restart-banner")

	sentence := p.text("#restart-summary")

	if sentence == "" {
		t.Fatal("the notice says nothing")
	}

	// The link is a word in it rather than a control beside it.
	var link string

	p.run("read the link", chromedp.Evaluate(
		`document.querySelector('#restart-summary #restart-open')?.textContent ?? ''`,
		&link))

	if link == "" {
		t.Fatal("the sentence carries no link")
	}

	if !strings.Contains(sentence, link) {
		t.Errorf("the link %q is not part of the sentence %q", link, sentence)
	}

	// And the sentence does not end on it: the word is inside, with the rest of
	// the sentence around it. A link that is the last thing is a button that has
	// been made to look like a word.
	if strings.TrimSpace(sentence) == strings.TrimSpace(link) {
		t.Error("the sentence is nothing but the link")
	}

	// Left-aligned text in a column that sits in the middle of the bar.
	var laidOut struct {
		Align   string  `json:"align"`
		Left    float64 `json:"left"`
		Right   float64 `json:"right"`
		BarWide float64 `json:"barWide"`
	}

	p.evalJSON(`(() => {
		const banner = document.querySelector('#restart-banner');
		const text = document.querySelector('#restart-summary');
		const bar = banner.getBoundingClientRect();
		const box = text.getBoundingClientRect();

		return JSON.stringify({
			align: window.getComputedStyle(banner).textAlign,
			left: box.left - bar.left,
			right: bar.right - box.right,
			barWide: bar.width,
		});
	})()`, &laidOut)

	if laidOut.Align != "left" {
		t.Errorf("the notice is set %q rather than flush left", laidOut.Align)
	}

	// The column is centred: the space on each side of it is about equal. Only
	// asked where the bar is wider than the column, which is the case the
	// centring exists for.
	if laidOut.BarWide > 900 {
		gap := laidOut.Left - laidOut.Right

		if gap < -40 || gap > 40 {
			t.Errorf("the column is not centred in the bar: %.0f on the left, "+
				"%.0f on the right", laidOut.Left, laidOut.Right)
		}
	}
}

// The connection type follows what is running, until somebody picks another.
//
// The type is not just a value on this card: it decides which other fields
// exist. Leaving it on whatever it happened to hold made the card describe a
// SQLite file directly under a line reading "currently connected via postgres" -
// two answers to the one question the card exists to answer.
//
// What left it there was the guard that protects the text fields. "Somebody is
// filling this form in" is the right question for a host somebody typed and the
// wrong one for this select, and a form counts as being filled in for reasons
// nobody chose - a draft restored from an earlier visit is one.
func TestTheConnectionTypeFollowsWhatIsRunningUntilSomebodyPicks(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-datasource", chromedp.ByID))

	p.waitForFilled("#datasource-active")

	running := p.value(`#form-datasource [name="dialect"]`)

	if running == "" {
		t.Fatal("the card shows no connection type at all")
	}

	// A type set the way a restored draft sets one: from a script, with the
	// change announced so the fields that depend on it are redrawn. Nobody
	// picked this.
	p.run("as a restored draft would", chromedp.Evaluate(`
		(() => {
			const dialect = document.querySelector('#form-datasource [name="dialect"]');

			dialect.value = dialect.value === 'postgres' ? 'mysql' : 'postgres';
			dialect.dispatchEvent(new Event('input', { bubbles: true }));
			dialect.dispatchEvent(new Event('change', { bubbles: true }));

			return dialect.value;
		})()`, nil))

	// Something reloads the screen, which happens for reasons having nothing to
	// do with the person in front of it.
	p.chooseLanguage("de")
	p.waitForText(`.tab[data-view="timesheets"]`, "Zeiteinträge")

	if got := p.value(`#form-datasource [name="dialect"]`); got != running {
		t.Errorf("the card shows %q while the installation runs %q", got, running)
	}

	// And a type somebody actually picks is theirs. Chosen with the keyboard,
	// because that is a real event: a value set from a script is what the case
	// above is about, and the difference between the two is the whole rule.
	p.run("pick one", chromedp.Focus(`#form-datasource [name="dialect"]`, chromedp.ByQuery),
		chromedp.SendKeys(`#form-datasource [name="dialect"]`, "m", chromedp.ByQuery))

	picked := p.value(`#form-datasource [name="dialect"]`)

	if picked == running {
		t.Skipf("the keyboard did not change the selection (still %q), so there is "+
			"nothing to protect from the next load", picked)
	}

	p.chooseLanguage("en")
	p.waitForText(`.tab[data-view="timesheets"]`, "Time entries")

	if got := p.value(`#form-datasource [name="dialect"]`); got != picked {
		t.Errorf("the type somebody picked (%q) was taken back to %q", picked, got)
	}
}
