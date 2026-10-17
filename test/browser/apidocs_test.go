//go:build browser

package browser

import (
	"testing"

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
