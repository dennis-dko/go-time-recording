//go:build browser

package browser

import (
	"fmt"
	"strings"

	"github.com/chromedp/chromedp"
)

// visible reports whether an element is on screen, as the browser sees it -
// not whether it exists in the markup.
// pixels reads a layout number off an element, for the cases where what is
// being asserted is that the page did not move.
func (p *page) pixels(selector, property string) int {
	p.t.Helper()

	var out int

	p.run("measure "+property+" of "+selector, chromedp.Evaluate(fmt.Sprintf(
		`Math.round(document.querySelector(%q)?.%s ?? -1)`, selector, property), &out))

	return out
}

// styleProperty reads an inline style set from script, which is how a page pins
// something for the duration of an action and then lets go of it.
func (p *page) styleProperty(selector, property string) string {
	p.t.Helper()

	var out string

	p.run("read style."+property+" of "+selector, chromedp.Evaluate(fmt.Sprintf(
		`document.querySelector(%q)?.style?.%s ?? ""`, selector, property), &out))

	return out
}

// disabled reports whether a control is refusing to be pressed.
//
// A button that says it is working is a button that has to stop saying it: the
// interesting half of "press it and it goes busy" is that it comes back.
func (p *page) disabled(selector string) bool {
	p.t.Helper()

	var result bool

	p.run("check whether "+selector+" is disabled", chromedp.Evaluate(fmt.Sprintf(
		`Boolean(document.querySelector(%q)?.disabled)`, selector), &result))

	return result
}

func (p *page) visible(selector string) bool {
	p.t.Helper()

	var result bool

	// checkVisibility rather than offsetParent: a position:fixed element always
	// has a null offsetParent, so the obvious check reports the sign-in overlay
	// as hidden while it covers the whole screen. The bounding box is the
	// fallback for browsers without it.
	p.run("check visibility of "+selector, chromedp.Evaluate(fmt.Sprintf(`
		(() => {
			const el = document.querySelector(%q);
			if (!el) return false;
			if (typeof el.checkVisibility === 'function') {
				return el.checkVisibility({ checkOpacity: true, checkVisibilityCSS: true });
			}
			const style = getComputedStyle(el);
			if (style.display === 'none' || style.visibility === 'hidden') return false;
			const box = el.getBoundingClientRect();
			return box.width > 0 && box.height > 0;
		})()`, selector), &result))

	return result
}

func (p *page) text(selector string) string {
	p.t.Helper()

	var out string

	p.run("read "+selector, chromedp.Evaluate(fmt.Sprintf(
		`document.querySelector(%q)?.textContent ?? ""`, selector), &out))

	return strings.TrimSpace(out)
}

// count is how many elements match, which text cannot answer.
//
// For the things the interface builds per row: a checkbox column derived from the
// rows is right or wrong by the number of checkboxes in it, and reading the table's
// text says nothing about that at all.
func (p *page) count(selector string) int {
	p.t.Helper()

	var out int

	p.run("count "+selector, chromedp.Evaluate(fmt.Sprintf(
		`document.querySelectorAll(%q).length`, selector), &out))

	return out
}

// attr reads an attribute as the browser currently has it, which is not always
// what the markup said: the reveal button changes an input's type in place.
// location is the address bar, which the interface writes the current screen
// into - so it is also what a sign-out has to let go of.
func (p *page) location() string {
	p.t.Helper()

	var out string

	p.run("read the address", chromedp.Evaluate(`location.href`, &out))

	return out
}

func (p *page) attr(selector, name string) string {
	p.t.Helper()

	var out string

	p.run(fmt.Sprintf("read %s of %s", name, selector), chromedp.Evaluate(fmt.Sprintf(
		`document.querySelector(%q)?.getAttribute(%q) ?? ""`, selector, name), &out))

	return strings.TrimSpace(out)
}

// locked reports whether a field refuses typing.
//
// Asked as a property, because the attribute cannot answer it. readonly and disabled
// are boolean attributes: present means true and their value is the empty string, so
// getAttribute returns "" whether the field is locked or wide open. The first version
// of this check compared that string against "" and therefore reported every field as
// editable - including one the interface had correctly locked, which is the expensive
// kind of wrong, because it sends somebody looking for a bug that is not there.
//
// Both mechanisms count. The question is whether anybody can type in the field; which
// of the two achieves that is the interface's business.
func (p *page) locked(selector string) bool {
	p.t.Helper()

	var out bool

	p.run("check whether "+selector+" refuses typing", chromedp.Evaluate(fmt.Sprintf(`
		(() => {
			const el = document.querySelector(%q);
			return Boolean(el && (el.readOnly || el.disabled));
		})()`, selector), &out))

	return out
}

// value reads what a form field holds, which text cannot: an input's content is
// its value, and textContent sees nothing there.
func (p *page) value(selector string) string {
	p.t.Helper()

	var out string

	p.run("read the value of "+selector, chromedp.Evaluate(fmt.Sprintf(
		`document.querySelector(%q)?.value ?? ""`, selector), &out))

	return strings.TrimSpace(out)
}

// placeholder reads what a field offers when it is empty.
//
// Its own reader beside value, because on this application's connection card
// the two mean opposite things: a value is what will be saved, and a
// placeholder is what is running and will not be.
func (p *page) placeholder(selector string) string {
	p.t.Helper()

	var out string

	p.run("read the placeholder of "+selector, chromedp.Evaluate(fmt.Sprintf(
		`document.querySelector(%q)?.placeholder ?? ""`, selector), &out))

	return strings.TrimSpace(out)
}
