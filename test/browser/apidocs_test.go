//go:build browser

package browser

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

// The API reference shows the specification's code and bold as code and bold.
//
// The descriptions in /openapi.json are CommonMark, as OpenAPI allows, so Swagger
// UI and Postman draw them; fifty-odd of them use `code` for a field, a header or
// a path, and **bold** for the word a sentence turns on. The reference rendered
// every one through textContent, and the page read "send `X-API-Token: <token>`"
// and "the owner's **current** role" with the marks in it.
func TestTheAPIReferenceShowsCodeAndBoldAsSuch(t *testing.T) {
	t.Parallel()

	p := open(t)

	p.run("open the reference", chromedp.Navigate(p.app.BaseURL()+"/api-docs"),
		chromedp.WaitVisible(".op", chromedp.ByQuery))

	var shown struct {
		Ticks  int `json:"ticks"`
		Stars  int `json:"stars"`
		Code   int `json:"code"`
		Strong int `json:"strong"`
	}

	p.evalJSON(`JSON.stringify((() => {
		const read = document.querySelector('#doc-description').innerText + document.querySelector('#doc-body').innerText;

		return {
			ticks: (read.match(/`+"`"+`/g) || []).length,
			stars: (read.match(/\*\*/g) || []).length,
			code: document.querySelectorAll('#doc-description code, #doc-body p code, #doc-body td code').length,
			strong: document.querySelectorAll('#doc-description strong, #doc-body strong').length,
		};
	})())`, &shown)

	if shown.Ticks > 0 || shown.Stars > 0 {
		t.Errorf("the reference shows %d backticks and %d pairs of asterisks as written, "+
			"where the specification means code and bold", shown.Ticks, shown.Stars)
	}

	if shown.Code == 0 || shown.Strong == 0 {
		t.Errorf("the reference draws %d pieces of code and %d of bold in its descriptions; "+
			"the specification has both", shown.Code, shown.Strong)
	}
}

// Back to the application returns to the tab the reference was opened from.
//
// The reference opens in a tab of its own from the token card, and its "Back to
// the application" loaded the application again in that tab: two of it open,
// and the one somebody came from still waiting where they had left it. Asked of
// the tabs the browser has: the reference's is gone afterwards, no other has
// opened, and the application's is the same page it was - a mark left in it
// before is still there, so it was returned to rather than loaded again.
func TestBackToTheApplicationReturnsToTheTabItCameFrom(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.run("open my account", chromedp.Evaluate(`switchView('settings')`, nil))
	p.waitShown(`#token-card a[href="/api-docs"]`)
	p.run("leave a mark", chromedp.Evaluate(`window.__stayed = true`, nil))

	opened := chromedp.WaitNewTarget(p.ctx, func(info *target.Info) bool {
		return strings.HasSuffix(info.URL, "/api-docs")
	})

	p.run("open the reference", p.click(`#token-card a[href="/api-docs"]`))

	var id target.ID

	select {
	case id = <-opened:
	case <-time.After(waitPatience):
		t.Fatal("the reference did not open in a tab of its own")
	}

	docs, cancel := chromedp.NewContext(p.ctx, chromedp.WithTargetID(id))
	defer cancel()

	// Waited for the reference itself, which its script draws: a press on the
	// link before that script has run is a press on a plain link.
	if err := chromedp.Run(docs, chromedp.WaitVisible(".op", chromedp.ByQuery)); err != nil {
		t.Fatalf("the reference never drew itself: %v", err)
	}

	// The tab closes under the press, which chromedp may report as the press
	// failing; what happened to the tabs is what is asked below.
	_ = chromedp.Run(docs, chromedp.Click(backToTheApplication, chromedp.ByQuery))

	p.waitFor("the reference's tab to close", func() bool {
		for _, page := range p.pages() {
			if page.TargetID == id {
				return false
			}
		}

		return true
	})

	if pages := p.pages(); len(pages) != 1 {
		urls := make([]string, 0, len(pages))
		for _, page := range pages {
			urls = append(urls, page.URL)
		}

		t.Errorf("after going back the browser has %d tabs: %v", len(pages), urls)
	}

	var stayed bool

	p.run("read the mark", chromedp.Evaluate(`window.__stayed === true`, &stayed))

	if !stayed {
		t.Error("the application was loaded again rather than returned to")
	}
}

// Opened on its own - a bookmark, a typed address - the reference has no tab to
// return to, and its way back is the link it always was: the application, in the
// same tab.
func TestBackToTheApplicationFromAReferenceOpenedOnItsOwnLoadsIt(t *testing.T) {
	t.Parallel()

	p := open(t)

	p.run("open the reference", chromedp.Navigate(p.app.BaseURL()+"/api-docs"),
		chromedp.WaitVisible(".op", chromedp.ByQuery))
	p.run("go back", chromedp.Click(backToTheApplication, chromedp.ByQuery),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	if pages := p.pages(); len(pages) != 1 {
		t.Errorf("going back from a reference opened on its own left %d tabs", len(pages))
	}
}

// backToTheApplication is the reference's way back, found by what it links to.
const backToTheApplication = `header nav a[href="/"]`

// pages lists the tabs the browser has open.
func (p *page) pages() []*target.Info {
	p.t.Helper()

	targets, err := chromedp.Targets(p.ctx)
	if err != nil {
		p.t.Fatalf("listing the tabs: %v", err)
	}

	pages := targets[:0]

	for _, info := range targets {
		if info.Type == "page" {
			pages = append(pages, info)
		}
	}

	return pages
}
