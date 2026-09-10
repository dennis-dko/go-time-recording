//go:build browser

package browser

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// The stylesheet has a global `[hidden] { display: none !important }` because
// layout rules on label and .login-screen otherwise beat the browser's own.
// Losing it makes hidden elements render, which is how the sign-in overlay got
// stuck.
func TestTheHiddenAttributeIsHonoured(t *testing.T) {
	t.Parallel()

	p := open(t)

	var rendered bool

	p.run("check a hidden element", chromedp.Evaluate(`
		(() => {
			const probe = document.createElement('label');
			probe.hidden = true;
			probe.textContent = 'probe';
			document.body.appendChild(probe);
			const shown = getComputedStyle(probe).display !== 'none';
			probe.remove();
			return shown;
		})()`, &rendered))

	if rendered {
		t.Error("a hidden element renders; the global [hidden] rule is not winning")
	}
}

// The whole interface is one script; if it throws on load, the page still
// renders and nothing works.
func TestTheScriptInitialisesWithoutThrowing(t *testing.T) {
	t.Parallel()

	p := open(t)

	if p.jsBroken() {
		t.Fatalf("app.js did not initialise\n\napplication log:\n%s", p.app.Log())
	}
}
