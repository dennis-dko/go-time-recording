//go:build browser

package browser

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// The configured logo is on the sign-in screen, not only in the header.
//
// Branding is fetched before anything has authenticated precisely so that this
// screen can carry the instance's own title and logo - somebody arriving at a
// company's time recording should see the company's mark rather than a default.
// Whether the image is actually on screen is a question only a browser answers:
// the element is in the markup, hidden, and it is the script that fills it in and
// unhides it, so nothing short of running the page can tell "wired up" from
// "wired up and working".
func TestTheConfiguredLogoIsOnTheSignInScreen(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	// A red rectangle, small enough to be a data URI in a test and unmistakable
	// on a screenshot if this ever needs one.
	// 600x120 - a five-to-one wordmark, which is the shape that makes cropping
	// visible: fitted it stays 5:1, filled it would be cut to the box.
	const logo = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAlgAAAB4CAYAAAAuVYzDAAADi0lEQVR4nOzd7U3cQBSGUROlFDqg/xLogF6IIn4ENlrWxq/n655TAPIdCc2j69Xurw0AgCiBBQAQJrAAAMIEFgBAmMACAAgTWAAAYQILACBMYAEAhAksAIAwgQUAECawAADCBBYAQJjAAgAIE1gAAGECCwAgTGABAIQJLACAMIEFABAmsAAAwgQWAECYwAIACPvd+wE45/Vtez/7N16et6fM0wAAf7lYJ5MIqkcEFwCc4yKdQIuoukdsAcBxLs+B9QyrW0ILAPZzaQ5opLC6JbQA4DGX5UBGDqtbQgsA7vM1DYOYKa62CZ8XAFoSWAOYNVZmfW4AuJrXPB2tFCheGQLAPzZYnawUV9uC8wDAGQKrg1VjZNW5AOAogdXY6hGy+nwAsIfAaqhKfFSZEwDuEViNVIuOavMCwGcCq4GqsVF1bgAQWAAAYQLrYtW3ONXnB6AmgXUhcfHBOQBQjcC6iKj4ynkAUInAAgAI8/txF7Ctuc9vFhLg/wsYng0WAECYwAqzvfqe8wGgAoEFABAmsIJsZ/ZxTgCsTmABAIQJLACAMIEV4rXXMc4LgJUJLACAMIEFABAmsAAAwgRWgM8T/YxzA2BVAgsAIExgAQCECSwAgDCBBQAQJrAAAMIEFgBAmMACAAgTWAAAYQILACBMYAEAhAksAIAwgQUAECawAADCBBYAQJjAAgAIE1gAAGECK+DleXvq/Qwzcm4ArEpgAQCECSwAgDCBBQAQJrBCfJ7oGOcFwMoEFgBAmMACAAgTWEFee+3jnABYncACAAgTWGG2M99zPgBUILAAAMJsEy7y+ra9936G0dheAVCFDRYAQJjAuohtzVfOA4BKBNaFRMUH5wBANQLrYtXjovr8ANQksAAAwgRWA1W3OFXnBgCB1Ui12Kg2LwB8JrAaqhIdVeYEgHsEVmOrx8fq8wHAHgKrg1UjZNW5AOAogdXJajGy2jwAcIZLcQAz/26hsAKA/9lgDWDWSJn1uQHgagJrELPFymzPCwAtuSQHNPIrQ2EFAI+5LAc2UmgJKwDYz6U5gZ6hJawA4DiX52RaxJaoAoBzXKSTSwSXoAIAAACG5msaAADCBBYAQJjAAgAIE1gAAGECCwAgTGABAIQJLACAMIEFABAmsAAAwgQWAECYwAIACBNYAABhAgsAIExgAQCECSwAgDCBBQAQJrAAAMIEFgBAmMACAAj7EwAA//8NjIji7L4NLAAAAABJRU5ErkJggg=="

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-branding", chromedp.ByID))

	// Through the API rather than the file picker: what is being checked is
	// whether a stored logo reaches the sign-in screen, and driving a file input
	// would be checking the picker instead.
	var status string

	p.run("store a logo", chromedp.Evaluate(`
		(async () => {
			const csrf = document.cookie.split(';').map(c => c.trim())
				.find(c => c.startsWith('gtr_csrf='))?.slice('gtr_csrf='.length) ?? '';

			const r = await fetch('/api/v1/settings/branding', {
				method: 'PUT',
				credentials: 'same-origin',
				headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
				body: JSON.stringify({ title: 'Zeiterfassung', logo: '`+logo+`' }),
			});

			return String(r.status);
		})()`, &status, awaitPromise))

	if status != "200" {
		t.Fatalf("could not store the logo: HTTP %s", status)
	}

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	// Reloaded, because branding is read once at start-up: without this the page
	// still holds what it fetched before the logo existed, and the test would be
	// asserting the state of a screen no visitor ever sees.
	p.run("reload the sign-in screen", chromedp.Reload(),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	if !p.visible("#login-logo") {
		t.Error("the configured logo is not shown on the sign-in screen")
	}

	if src := p.attr("#login-logo", "src"); !strings.HasPrefix(src, "data:image/") {
		t.Errorf("the sign-in logo has no image behind it: %.40q", src)
	}

	// And it is given the room a sign-in screen has, rather than the 28px that
	// fits beside the navigation. The same image serves both, so the only thing
	// separating a banner from a favicon-sized mark is what each place allows it.
	var box struct {
		Width   float64 `json:"width"`
		Height  float64 `json:"height"`
		Natural float64 `json:"natural"`
	}

	p.run("measure the banner", chromedp.Evaluate(
		`(() => {
			const el = document.querySelector('#login-logo');
			const r = el.getBoundingClientRect();
			return { width: r.width, height: r.height,
				natural: el.naturalWidth / el.naturalHeight };
		})()`, &box))

	if box.Height <= 40 {
		t.Errorf("the sign-in logo is %.0fpx tall, which is the header's size - it is "+
			"meant to be a banner there", box.Height)
	}

	// Nothing is cut, asked as a property of the image rather than of the fixture:
	// the box it renders in has the shape the image itself has. A box wider or
	// taller than that is a box the image has been filled into, and filling a
	// 5:1 wordmark into anything squarer slices the top and bottom off it.
	//
	// A first version compared the box against a hard-coded 3:1 and passed with
	// object-fit: cover, which crops - the box was still 328x96, because the box
	// is what CSS said and not what was drawn in it.
	if ratio := box.Width / box.Height; ratio < box.Natural*0.95 {
		t.Errorf("the banner renders %.0fx%.0f, a ratio of %.2f, from an image whose "+
			"own ratio is %.2f - it has been cropped to the box rather than fitted "+
			"into it", box.Width, box.Height, ratio, box.Natural)
	}

	// The title travels with it, so a wrong one here would mean branding loaded
	// and only the image failed - a different fault worth telling apart.
	if got := p.text("#login-screen h2"); got == "" {
		t.Error("the sign-in card lost its heading, so this screen did not render")
	}

	// The same image is the tab icon, fetched from the address the server wrote
	// into the document. Asked of what the address answers with rather than of the
	// address itself: what a browser draws in a tab is the bytes, and an href that
	// looks right while serving the shipped mark is exactly the failure this went
	// through twice.
	if !p.iconIsTheLogo(t) {
		t.Error("the browser tab is not served the configured logo")
	}

	// And it is served converted rather than passed through: 64px square,
	// whatever was uploaded. A wordmark handed to a browser at its own size is
	// what left the decision to the browser, which is where the tab icon kept
	// going wrong.
	if width := p.iconWidth(t); width != 64 {
		t.Errorf("the tab is served a %dpx icon; a converted one is 64 square", width)
	}
}

// Clearing the logo puts the shipped mark back in the tab.
//
// The other half, and the half that fails silently: an installation that tries a
// logo and takes it off again would keep the old one for as long as anything
// cached it. The rule asked for was the logo "otherwise always the default as
// before", which is a statement about both directions.
func TestClearingTheLogoRestoresTheDefaultFavicon(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	const logo = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAlgAAAB4CAYAAAAuVYzDAAADi0lEQVR4nOzd7U3cQBSGUROlFDqg/xLogF6IIn4ENlrWxq/n655TAPIdCc2j69Xurw0AgCiBBQAQJrAAAMIEFgBAmMACAAgTWAAAYQILACBMYAEAhAksAIAwgQUAECawAADCBBYAQJjAAgAIE1gAAGECCwAgTGABAIQJLACAMIEFABAmsAAAwgQWAECYwAIACPvd+wE45/Vtez/7N16et6fM0wAAf7lYJ5MIqkcEFwCc4yKdQIuoukdsAcBxLs+B9QyrW0ILAPZzaQ5opLC6JbQA4DGX5UBGDqtbQgsA7vM1DYOYKa62CZ8XAFoSWAOYNVZmfW4AuJrXPB2tFCheGQLAPzZYnawUV9uC8wDAGQKrg1VjZNW5AOAogdXY6hGy+nwAsIfAaqhKfFSZEwDuEViNVIuOavMCwGcCq4GqsVF1bgAQWAAAYQLrYtW3ONXnB6AmgXUhcfHBOQBQjcC6iKj4ynkAUInAAgAI8/txF7Ctuc9vFhLg/wsYng0WAECYwAqzvfqe8wGgAoEFABAmsIJsZ/ZxTgCsTmABAIQJLACAMIEV4rXXMc4LgJUJLACAMIEFABAmsAAAwgRWgM8T/YxzA2BVAgsAIExgAQCECSwAgDCBBQAQJrAAAMIEFgBAmMACAAgTWAAAYQILACBMYAEAhAksAIAwgQUAECawAADCBBYAQJjAAgAIE1gAAGECK+DleXvq/Qwzcm4ArEpgAQCECSwAgDCBBQAQJrBCfJ7oGOcFwMoEFgBAmMACAAgTWEFee+3jnABYncACAAgTWGG2M99zPgBUILAAAMJsEy7y+ra9936G0dheAVCFDRYAQJjAuohtzVfOA4BKBNaFRMUH5wBANQLrYtXjovr8ANQksAAAwgRWA1W3OFXnBgCB1Ui12Kg2LwB8JrAaqhIdVeYEgHsEVmOrx8fq8wHAHgKrg1UjZNW5AOAogdXJajGy2jwAcIZLcQAz/26hsAKA/9lgDWDWSJn1uQHgagJrELPFymzPCwAtuSQHNPIrQ2EFAI+5LAc2UmgJKwDYz6U5gZ6hJawA4DiX52RaxJaoAoBzXKSTSwSXoAIAAACG5msaAADCBBYAQJjAAgAIE1gAAGECCwAgTGABAIQJLACAMIEFABAmsAAAwgQWAECYwAIACBNYAABhAgsAIExgAQCECSwAgDCBBQAQJrAAAMIEFgBAmMACAAj7EwAA//8NjIji7L4NLAAAAABJRU5ErkJggg=="

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-branding", chromedp.ByID))

	p.storeBranding(t, logo)
	p.run("reload with the logo", chromedp.Reload(),
		chromedp.WaitVisible("#who", chromedp.ByID))

	if !p.iconIsTheLogo(t) {
		t.Fatal("the logo never reached the tab, so this case cannot show it " +
			"being cleared")
	}

	p.storeBranding(t, "")
	p.run("reload without it", chromedp.Reload(),
		chromedp.WaitVisible("#who", chromedp.ByID))

	if p.iconIsTheLogo(t) {
		t.Error("after clearing the logo the tab is still served it")
	}

	// And it is served something rather than nothing: an instance with no logo
	// keeps the shipped mark.
	if kind := p.iconContentType(t); !strings.Contains(kind, "svg") {
		t.Errorf("with no logo the tab icon is served as %q", kind)
	}
}

// iconIsTheLogo fetches whatever the document points its icon at and reports
// whether it is the image this case stored.
//
// The bytes rather than the address. Two attempts at this bug looked correct in
// the DOM and wrong in the tab, and both times the test was reading the element
// that had been changed rather than the picture that had not.
func (p *page) iconIsTheLogo(t *testing.T) bool {
	t.Helper()

	var kind string

	p.run("fetch the tab icon", chromedp.Evaluate(`
		(async () => {
			const link = document.querySelector('link[rel~="icon"]');
			if (!link) return 'no icon declared';

			const r = await fetch(link.href);
			return r.headers.get('content-type') ?? '';
		})()`, &kind, awaitPromise))

	// The type tells them apart now. A configured logo is converted into a square
	// PNG before it is served - see toIcon for why - and the shipped mark is the
	// SVG that ships with the application, so what comes back says which of the
	// two the tab is being given.
	return strings.Contains(kind, "image/png")
}

// iconWidth is the intrinsic width of whatever the tab icon is served as.
//
// A converted logo is always square and small, so this is what proves the
// conversion happened at all rather than the original having been passed
// through.
func (p *page) iconWidth(t *testing.T) int {
	t.Helper()

	var width int

	p.run("measure the tab icon", chromedp.Evaluate(`
		(async () => {
			const link = document.querySelector('link[rel~="icon"]');
			if (!link) return 0;

			return await new Promise((resolve) => {
				const probe = new Image();
				probe.onload = () => resolve(probe.naturalWidth);
				probe.onerror = () => resolve(0);
				probe.src = link.href;
			});
		})()`, &width, awaitPromise))

	return width
}

// iconContentType is what the tab icon is served as.
func (p *page) iconContentType(t *testing.T) string {
	t.Helper()

	var kind string

	p.run("read the icon's type", chromedp.Evaluate(`
		(async () => {
			const link = document.querySelector('link[rel~="icon"]');
			if (!link) return '';

			const r = await fetch(link.href);
			return r.headers.get('content-type') ?? '';
		})()`, &kind, awaitPromise))

	return kind
}

// storeBranding writes the instance's logo through the API, as the Settings form
// does. An empty string clears it.
func (p *page) storeBranding(t *testing.T, logo string) {
	t.Helper()

	var status string

	p.run("store the branding", chromedp.Evaluate(`
		(async () => {
			const csrf = document.cookie.split(';').map(c => c.trim())
				.find(c => c.startsWith('gtr_csrf='))?.slice('gtr_csrf='.length) ?? '';

			const r = await fetch('/api/v1/settings/branding', {
				method: 'PUT',
				credentials: 'same-origin',
				headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
				body: JSON.stringify({ title: 'Zeiterfassung', logo: '`+logo+`' }),
			});

			return String(r.status);
		})()`, &status, awaitPromise))

	if status != "200" {
		t.Fatalf("could not store the branding: HTTP %s", status)
	}
}

// Clearing the logo puts the shipped mark back at once, not at the next reload.
//
// The reload case was already covered and passed, which is what let this
// through: the document the server sends is correct the moment it is asked for,
// so anything that reloads sees the right icon whatever the page did. What
// somebody actually does is press Save and look at the tab.
func TestClearingTheLogoChangesTheTabWithoutAReload(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	const logo = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAlgAAAB4CAYAAAAuVYzDAAADi0lEQVR4nOzd7U3cQBSGUROlFDqg/xLogF6IIn4ENlrWxq/n655TAPIdCc2j69Xurw0AgCiBBQAQJrAAAMIEFgBAmMACAAgTWAAAYQILACBMYAEAhAksAIAwgQUAECawAADCBBYAQJjAAgAIE1gAAGECCwAgTGABAIQJLACAMIEFABAmsAAAwgQWAECYwAIACPvd+wE45/Vtez/7N16et6fM0wAAf7lYJ5MIqkcEFwCc4yKdQIuoukdsAcBxLs+B9QyrW0ILAPZzaQ5opLC6JbQA4DGX5UBGDqtbQgsA7vM1DYOYKa62CZ8XAFoSWAOYNVZmfW4AuJrXPB2tFCheGQLAPzZYnawUV9uC8wDAGQKrg1VjZNW5AOAogdXY6hGy+nwAsIfAaqhKfFSZEwDuEViNVIuOavMCwGcCq4GqsVF1bgAQWAAAYQLrYtW3ONXnB6AmgXUhcfHBOQBQjcC6iKj4ynkAUInAAgAI8/txF7Ctuc9vFhLg/wsYng0WAECYwAqzvfqe8wGgAoEFABAmsIJsZ/ZxTgCsTmABAIQJLACAMIEV4rXXMc4LgJUJLACAMIEFABAmsAAAwgRWgM8T/YxzA2BVAgsAIExgAQCECSwAgDCBBQAQJrAAAMIEFgBAmMACAAgTWAAAYQILACBMYAEAhAksAIAwgQUAECawAADCBBYAQJjAAgAIE1gAAGECK+DleXvq/Qwzcm4ArEpgAQCECSwAgDCBBQAQJrBCfJ7oGOcFwMoEFgBAmMACAAgTWEFee+3jnABYncACAAgTWGG2M99zPgBUILAAAMJsEy7y+ra9936G0dheAVCFDRYAQJjAuohtzVfOA4BKBNaFRMUH5wBANQLrYtXjovr8ANQksAAAwgRWA1W3OFXnBgCB1Ui12Kg2LwB8JrAaqhIdVeYEgHsEVmOrx8fq8wHAHgKrg1UjZNW5AOAogdXJajGy2jwAcIZLcQAz/26hsAKA/9lgDWDWSJn1uQHgagJrELPFymzPCwAtuSQHNPIrQ2EFAI+5LAc2UmgJKwDYz6U5gZ6hJawA4DiX52RaxJaoAoBzXKSTSwSXoAIAAACG5msaAADCBBYAQJjAAgAIE1gAAGECCwAgTGABAIQJLACAMIEFABAmsAAAwgQWAECYwAIACBNYAABhAgsAIExgAQCECSwAgDCBBQAQJrAAAMIEFgBAmMACAAj7EwAA//8NjIji7L4NLAAAAABJRU5ErkJggg=="

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-branding", chromedp.ByID))

	p.storeBranding(t, logo)
	p.run("reload with the logo", chromedp.Reload(),
		chromedp.WaitVisible("#who", chromedp.ByID))

	if !p.iconIsTheLogo(t) {
		t.Fatal("the logo never reached the tab, so this case cannot show it going")
	}

	// Saved through the form's own path rather than by reloading afterwards, which
	// is the whole point: the page has to notice.
	p.run("open Settings again", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-branding", chromedp.ByID))

	p.run("take the logo off", p.click("#logo-clear"),
		p.click(`#form-branding button[type="submit"]`))

	// Not waited for by its notice: saving a changed mark reloads the page, and
	// the notice goes with it. What this case is about is the tab, so that is what
	// is waited for - polled, because the save, the branding behind it and the
	// reload all have to finish first.
	deadline := time.Now().Add(waitPatience)

	for p.iconIsTheLogo(t) {
		if time.Now().After(deadline) {
			t.Fatal("the tab still shows the logo after it was removed and saved; " +
				"it only changes on the next reload")
		}

		time.Sleep(200 * time.Millisecond)
	}
}

// The two places a company's own mark goes stay empty until it puts one there.
//
// They fell back to the shipped mark for a while, on the reasoning that a header
// of words alone looks unfinished. It is the wrong place for it: filling the
// slots meant for whoever runs the installation makes an unbranded one look
// branded by somebody else. The application's own mark has its own places - the
// browser tab, and the button beside the title - which say which program this is
// without taking the space meant for the company.
func TestTheLogoSlotsStayEmptyUntilALogoIsConfigured(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	// Nothing configured: this instance has never been given a logo.
	//
	// Asked of the source rather than of visibility alone, because the sign-in
	// screen is hidden while somebody is signed in - what matters is that there
	// is nothing waiting in it for the next visitor.
	for _, holder := range []string{"#brand-logo", "#login-logo"} {
		if src := p.attr(holder, "src"); src != "" {
			t.Errorf("%s shows %.40q on an installation with no logo", holder, src)
		}
	}

	if p.visible("#brand-logo") {
		t.Error("the header shows a mark on an installation that has configured none")
	}

	// Something stands in the slot all the same: the installation's initial,
	// taken from the title it was given. The corner was empty until somebody
	// configured a logo, which is a fresh installation saying nothing about
	// itself.
	if !p.visible("#brand-mark") {
		t.Error("the logo slot is empty on an installation that has uploaded none, " +
			"so the corner says nothing about which installation this is")
	}

	if letter := p.text("#brand-initial"); letter == "" {
		t.Error("the placeholder mark carries no letter, so it is an empty chip")
	}

	// And the application's own mark is beside the title, where it belongs.
	//
	// The two used to share one drawing and show one of it, so a fresh
	// installation wore the application's mark in the logo slot and had a bare
	// title in the middle - the title losing its mark to a slot that is not
	// about the application at all. Both are filled now, and they say different
	// things: the left one is this installation, the middle one is this program.
	if !p.visible(".app-mark") {
		t.Error("the title has no mark beside it, so nothing in the middle of the " +
			"bar says which application this is")
	}

	// Drawn, not typed. The house character it replaced is a font glyph: a
	// different picture on every platform and a hollow box where the font has no
	// house in it.
	var marks int

	p.evalJSON(`JSON.stringify(document.querySelectorAll('.app-mark svg, svg.app-mark').length)`,
		&marks)

	if marks == 0 {
		t.Error("the mark beside the title is not drawn")
	}

	// And a configured logo fills the header.
	const logo = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAlgAAAB4CAYAAAAuVYzDAAADi0lEQVR4nOzd7U3cQBSGUROlFDqg/xLogF6IIn4ENlrWxq/n655TAPIdCc2j69Xurw0AgCiBBQAQJrAAAMIEFgBAmMACAAgTWAAAYQILACBMYAEAhAksAIAwgQUAECawAADCBBYAQJjAAgAIE1gAAGECCwAgTGABAIQJLACAMIEFABAmsAAAwgQWAECYwAIACPvd+wE45/Vtez/7N16et6fM0wAAf7lYJ5MIqkcEFwCc4yKdQIuoukdsAcBxLs+B9QyrW0ILAPZzaQ5opLC6JbQA4DGX5UBGDqtbQgsA7vM1DYOYKa62CZ8XAFoSWAOYNVZmfW4AuJrXPB2tFCheGQLAPzZYnawUV9uC8wDAGQKrg1VjZNW5AOAogdXY6hGy+nwAsIfAaqhKfFSZEwDuEViNVIuOavMCwGcCq4GqsVF1bgAQWAAAYQLrYtW3ONXnB6AmgXUhcfHBOQBQjcC6iKj4ynkAUInAAgAI8/txF7Ctuc9vFhLg/wsYng0WAECYwAqzvfqe8wGgAoEFABAmsIJsZ/ZxTgCsTmABAIQJLACAMIEV4rXXMc4LgJUJLACAMIEFABAmsAAAwgRWgM8T/YxzA2BVAgsAIExgAQCECSwAgDCBBQAQJrAAAMIEFgBAmMACAAgTWAAAYQILACBMYAEAhAksAIAwgQUAECawAADCBBYAQJjAAgAIE1gAAGECK+DleXvq/Qwzcm4ArEpgAQCECSwAgDCBBQAQJrBCfJ7oGOcFwMoEFgBAmMACAAgTWEFee+3jnABYncACAAgTWGG2M99zPgBUILAAAMJsEy7y+ra9936G0dheAVCFDRYAQJjAuohtzVfOA4BKBNaFRMUH5wBANQLrYtXjovr8ANQksAAAwgRWA1W3OFXnBgCB1Ui12Kg2LwB8JrAaqhIdVeYEgHsEVmOrx8fq8wHAHgKrg1UjZNW5AOAogdXJajGy2jwAcIZLcQAz/26hsAKA/9lgDWDWSJn1uQHgagJrELPFymzPCwAtuSQHNPIrQ2EFAI+5LAc2UmgJKwDYz6U5gZ6hJawA4DiX52RaxJaoAoBzXKSTSwSXoAIAAACG5msaAADCBBYAQJjAAgAIE1gAAGECCwAgTGABAIQJLACAMIEFABAmsAAAwgQWAECYwAIACBNYAABhAgsAIExgAQCECSwAgDCBBQAQJrAAAMIEFgBAmMACAAj7EwAA//8NjIji7L4NLAAAAABJRU5ErkJggg=="

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-branding", chromedp.ByID))

	p.storeBranding(t, logo)

	p.run("reload", chromedp.Reload(), chromedp.WaitVisible("#who", chromedp.ByID))

	if src := p.attr("#brand-logo", "src"); !strings.HasPrefix(src, "data:image/") {
		t.Errorf("the header shows %.40q after a logo was configured", src)
	}

	if !p.visible("#brand-logo") {
		t.Error("the header's mark is hidden after a logo was configured")
	}

	// The mark beside the title is untouched by any of this: it is the
	// application, and the application is there whatever the installation has
	// uploaded.
	if !p.visible(".app-mark") {
		t.Error("nothing on the bar says which application this is once the logo " +
			"slot belongs to the installation")
	}

	// The placeholder steps aside, though - that is what it is for.
	if p.visible("#brand-mark") {
		t.Error("the placeholder initial is still in the logo slot beside the logo " +
			"that replaced it")
	}

	// The logo at the left end and the name in the middle, which is the way round
	// they were swapped to. The account keeps the right.
	var placed struct {
		TitleLeft float64 `json:"titleLeft"`
		LogoLeft  float64 `json:"logoLeft"`
		Centre    float64 `json:"centre"`
		BarCentre float64 `json:"barCentre"`
	}

	p.evalJSON(`JSON.stringify((() => {
		const title = document.querySelector('#app-title').getBoundingClientRect();
		const logo = document.querySelector('#brand-logo').getBoundingClientRect();
		const bar = document.querySelector('.topbar').getBoundingClientRect();

		return {
			titleLeft: title.left,
			logoLeft: logo.left,
			// The name itself, not the column holding it.
			centre: title.left + title.width / 2,
			barCentre: bar.left + bar.width / 2,
		};
	})())`, &placed)

	if placed.TitleLeft < placed.LogoLeft {
		t.Errorf("the title is at %.0f and the logo at %.0f, so the words come "+
			"before the picture", placed.TitleLeft, placed.LogoLeft)
	}

	if math.Abs(placed.Centre-placed.BarCentre) > 2 {
		t.Errorf("the name sits at %.0f and the bar's middle is %.0f",
			placed.Centre, placed.BarCentre)
	}

	// The room the sign-in screen gives it, which is what it is given here now.
	// It was 40px in the bar and 96 on the card - two sizes for one picture, and
	// the header's was the one nobody could read.
	var sizes struct {
		Header float64 `json:"header"`
		SignIn float64 `json:"signIn"`
		Source string  `json:"source"`
	}

	p.evalJSON(`JSON.stringify((() => {
		const header = document.querySelector('#brand-logo');
		const login = document.querySelector('#login-logo');

		return {
			header: parseFloat(getComputedStyle(header).maxHeight),
			signIn: parseFloat(getComputedStyle(login).maxHeight),
			// Both are handed the same copy: the header's slot is no longer the
			// small derived one, which would be enlarged and blurred.
			source: header.src === login.src ? 'same' : 'different',
		};
	})())`, &sizes)

	if sizes.Header != sizes.SignIn {
		t.Errorf("the header allows the logo %.0fpx and the sign-in screen %.0fpx",
			sizes.Header, sizes.SignIn)
	}

	if sizes.Source != "same" {
		t.Error("the header is given a different copy of the logo from the sign-in " +
			"screen, so one of the two is being enlarged")
	}
}

// Saving keeps somebody where they were on the page.
//
// A changed mark reloads - no engine takes a new tab icon from a link swapped in
// afterwards - and the reload used to land at the top. The appearance settings
// are a long way down a long screen, so every save meant scrolling back down to
// see whether what was just saved looks right.
func TestSavingTheLogoKeepsThePlaceOnThePage(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-branding", chromedp.ByID))

	p.storeBranding(t, wideLogo)

	p.run("reload", chromedp.Reload(), chromedp.WaitVisible("#who", chromedp.ByID))
	p.run("open Settings again", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-branding", chromedp.ByID))

	// A different part of the logo for the tab, which is a changed mark and
	// therefore a save that reloads.
	p.run("choose a part", p.click(`.logo-use-button[data-crop="icon"]`),
		chromedp.WaitVisible("#crop-overlay", chromedp.ByID))
	p.drag(`.crop-handle-se`, 0, -40)
	p.run("use it", p.click("#crop-apply"))

	// Somewhere down the page, and submitted without pressing the button: a click
	// scrolls its target into view, which would move the very thing being
	// measured.
	var from float64

	p.evalJSON(`JSON.stringify((() => {
		window.scrollTo(0, 700);

		return window.scrollY;
	})())`, &from)

	if from < 100 {
		t.Skipf("the settings screen is only %.0fpx of scroll here, so there is "+
			"nowhere to be put back to", from)
	}

	p.submitAndAwaitReload(t, chromedp.Evaluate(
		`document.querySelector('#form-branding').requestSubmit()`, nil))

	// Given a moment to be put back: the page grows as each panel answers, and
	// the scroll lands once there is enough page to land on.
	var landed float64

	settled := time.Now().Add(waitPatience)

	for {
		p.evalJSON(`JSON.stringify(window.scrollY)`, &landed)

		if math.Abs(landed-from) <= 40 || time.Now().After(settled) {
			break
		}

		time.Sleep(250 * time.Millisecond)
	}

	if math.Abs(landed-from) > 40 {
		t.Errorf("the save left the page at %.0f, having been at %.0f - somebody "+
			"has to scroll back down to what they just changed", landed, from)
	}
}
