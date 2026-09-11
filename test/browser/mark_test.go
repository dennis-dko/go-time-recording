//go:build browser

package browser

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// An installation that has uploaded no logo still has a mark.
//
// The left end of the bar was simply empty until somebody configured one, which
// is a corner of the screen saying nothing on every fresh installation. The
// application ships one now - drawn rather than fetched, so it costs no request
// and takes the reader's own theme colours - and it steps aside the moment a
// real logo is configured, because that one is the installation's identity and
// this one is only a stand-in for it.
func TestAnInstallationWithNoLogoStillHasAMark(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	if !p.visible("#brand-mark") {
		t.Error("an installation with no logo has an empty corner where a mark " +
			"would be")
	}

	// The same drawing as the one beside the title, referenced rather than
	// repeated: two copies of one mark are two drawings that can drift apart.
	var uses string

	p.run("how the mark is drawn", chromedp.Evaluate(`
		JSON.stringify({
			title: document.querySelector('.app-mark use')?.getAttribute('href') ?? '',
			symbols: document.querySelectorAll('symbol#icon-mark').length,
			initial: document.querySelector('#brand-initial')?.textContent ?? '',
		})`, &uses))

	// The application's mark is referenced from the one definition of it. It is
	// used beside the title and by nothing else now, which is one use - but a
	// drawing written out where it is used is a drawing that can be changed in
	// one place and not the other, and this is the check that noticed.
	if !strings.Contains(uses, `"title":"#icon-mark"`) {
		t.Errorf("the mark is drawn out where it is used rather than referenced: %s", uses)
	}

	if !strings.Contains(uses, `"symbols":1`) {
		t.Errorf("the mark is defined more or less than once: %s", uses)
	}

	// The placeholder is not that drawing. It is the installation's initial, and
	// it is the letter that makes it this installation's rather than anyone's.
	if strings.Contains(uses, `"initial":""`) {
		t.Errorf("the logo slot holds no initial: %s", uses)
	}

	// A logo of its own. One pixel is enough: what is being asked is which of the
	// two is on screen, not what either looks like.
	const dot = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-branding", chromedp.ByID))
	p.settled()

	var saved string

	p.run("configure a logo", chromedp.Evaluate(`
		(async () => {
			const csrf = document.cookie.split(';').map((c) => c.trim())
				.find((c) => c.startsWith('gtr_csrf='))?.slice('gtr_csrf='.length) ?? '';

			const res = await fetch('/api/v1/settings/branding', {
				method: 'PUT',
				credentials: 'same-origin',
				headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
				body: JSON.stringify({ title: 'Alpha GmbH', logo: '`+dot+`' }),
			});

			await loadBranding();

			return String(res.status);
		})()`, &saved, awaitPromise))

	if saved != "200" {
		t.Fatalf("the logo was not accepted (%s), so this case is about to prove nothing",
			saved)
	}

	p.waitGone("#brand-mark")

	if !p.visible("#brand-logo") {
		t.Error("the configured logo is not on screen, so the mark stepped aside " +
			"for nothing")
	}
}

// The sign-in screen stands in for a logo the same way the header does.
//
// An installation that has uploaded no logo showed nothing at all above the
// heading, which reads as a page that has not finished loading rather than as an
// installation without a logo. The header has carried a lettered chip for this
// all along; the sign-in card is the one screen everybody sees first, and it had
// none.
func TestTheSignInScreenShowsTheLetteredMarkWhenThereIsNoLogo(t *testing.T) {
	t.Parallel()

	p := open(t)

	// Before signing in: this is the screen the mark belongs on.
	if !p.visible("#login-mark") {
		t.Fatal("the sign-in screen shows no mark where a logo would be")
	}

	// The letter is the initial of the configured title, so it says something
	// about this installation rather than being a decoration.
	letter := strings.TrimSpace(p.text("#login-initial"))
	if letter == "" {
		t.Error("the mark on the sign-in screen carries no letter")
	}

	if len([]rune(letter)) != 1 {
		t.Errorf("the mark should carry one letter, it carries %q", letter)
	}

	// And it is the same letter the header uses, rather than a second answer to
	// the same question.
	p.readyAdmin()

	if inHeader := strings.TrimSpace(p.text("#brand-initial")); inHeader != letter {
		t.Errorf("the sign-in screen says %q and the header says %q", letter, inHeader)
	}
}
