//go:build browser

package browser

import (
	"fmt"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// No flex basis meant as a width becomes empty height on a telephone, on any
// screen.
//
// A basis is measured along the line, so the one a control is given to share a
// row - the live log's search box had 200px - becomes its height wherever a
// narrow layout turns the row into a column. Nothing in the stylesheet says which
// rows turn, and the box only looks wrong on a screen nobody was measuring: the
// log's search box was found in a screenshot from a telephone, 144px of nothing
// above the refresh interval. So the sign-in and every screen of an administrator
// and of somebody who records time are walked at telephone width, and every flex
// item in a column whose basis is a length is asked how much of its height its
// content does not use.
func TestNoFlexBasisBecomesAnEmptyHeightOnATelephone(t *testing.T) {
	t.Parallel()

	p := open(t)

	var found []string

	measure := func(where string) {
		p.run("a telephone", chromedp.EmulateViewport(390, 844))

		var empty []string

		p.run("measure "+where, chromedp.Evaluate(`(() => {
			const describe = el => el.tagName.toLowerCase() +
				(el.id ? '#' + el.id : '') +
				[...el.classList].map(name => '.' + name).join('');
			const found = [];
			for (const el of document.querySelectorAll('body *')) {
				const parent = el.parentElement;
				if (!parent || el.getClientRects().length === 0) continue;
				const outer = getComputedStyle(parent);
				if (!outer.display.includes('flex') || !outer.flexDirection.startsWith('column')) continue;
				const style = getComputedStyle(el);
				if (!style.flexBasis.endsWith('px') || parseFloat(style.flexBasis) <= 0) continue;
				const range = document.createRange();
				range.selectNodeContents(el);
				const used = range.getBoundingClientRect().height;
				const inner = el.getBoundingClientRect().height -
					parseFloat(style.paddingTop) - parseFloat(style.paddingBottom) -
					parseFloat(style.borderTopWidth) - parseFloat(style.borderBottomWidth);
				if (inner - used > 24) found.push(describe(el) + ': ' + Math.round(inner - used) + 'px empty');
			}
			return found;
		})()`, &empty))

		for _, e := range empty {
			found = append(found, where+": "+e)
		}

		// Back to a width where the tabs are on the bar rather than behind the
		// menu, for whatever is pressed next.
		p.run("a desktop", chromedp.EmulateViewport(1280, 900))
	}

	walk := func(who string) {
		var views []string

		p.run("list "+who+"'s screens", chromedp.Evaluate(
			`[...document.querySelectorAll('.tab[data-view]')].filter(tab => !tab.hidden).map(tab => tab.dataset.view)`,
			&views))

		if len(views) < 2 {
			t.Fatalf("too few screens to walk for %s, which says the tabs were not found: %v", who, views)
		}

		t.Logf("walking the %s's screens: %v", who, views)

		for _, view := range views {
			p.run("open "+view, p.click(fmt.Sprintf(`.tab[data-view=%q]`, view)))
			measure(who + " " + view)
		}
	}

	measure("sign-in")

	p.readyAdmin()
	walk("administrator")

	p.becomeWorker()
	walk("worker")

	if len(found) > 0 {
		t.Errorf("on a 390px screen a flex basis meant as a width leaves height empty:\n\t%s",
			strings.Join(found, "\n\t"))
	}
}
