//go:build browser

package browser

import (
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
