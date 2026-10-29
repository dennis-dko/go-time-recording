//go:build browser

package browser

import (
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/dennis-dko/go-time-recording/internal/support/qrcode"
)

// The card that offers to install the page as an app shows the page's own
// address as a code, for opening it on a phone without typing the address - the
// address this page is at, exactly.
func TestTheInstallCardShowsThisAddressAsACode(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.run("open my account", p.click(`.tab[data-view="settings"]`),
		chromedp.WaitVisible("#install-card", chromedp.ByID))
	p.waitShown("#install-qr")

	var shown struct {
		Origin string `json:"origin"`
		Src    string `json:"src"`
	}

	p.awaitJSON(`Promise.resolve({
		origin: location.origin,
		src: document.querySelector('#install-qr').getAttribute('src') ?? '',
	})`, &shown)

	if want, err := qrcode.SVGDataURI(shown.Origin); err != nil || shown.Src != want {
		t.Errorf("the code on the install card is not the code of %s (src %.40q)", shown.Origin, shown.Src)
	}

	if !p.visible("#install-phone") {
		t.Error("the code stands on the card without the sentence saying what it is for")
	}
}
