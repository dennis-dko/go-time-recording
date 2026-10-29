//go:build integration

package integration

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/support/qrcode"
)

// The installation's address comes as a code as well, for opening it on a phone
// without typing it: the origin the page names, where that is this installation,
// and the address as reached otherwise - so a page cannot have a code drawn of
// somewhere else - and for somebody signed in only.
func TestTheInstallationsAddressComesAsACode(t *testing.T) {
	t.Parallel()

	a, _, worker := startWithWorker(t)

	var answer struct {
		Address string `json:"address"`
		QR      string `json:"qr"`
	}

	worker.must(worker.api(http.MethodGet, "/me/address?origin="+url.QueryEscape(a.BaseURL()), nil),
		http.StatusOK).Data(t, &answer)

	if answer.Address != a.BaseURL() {
		t.Errorf("the address is %q; the page is at %q", answer.Address, a.BaseURL())
	}

	if want, err := qrcode.SVGDataURI(answer.Address); err != nil || answer.QR != want {
		t.Errorf("the code is not the code of the address (%d characters, want %d)", len(answer.QR), len(want))
	}

	worker.must(worker.api(http.MethodGet, "/me/address?origin="+url.QueryEscape("https://elsewhere.example"), nil),
		http.StatusOK).Data(t, &answer)

	if answer.Address != a.BaseURL() {
		t.Errorf("asked with another host's origin, the address is %q, want this installation's %q",
			answer.Address, a.BaseURL())
	}

	if anonymous := a.newClient().do(http.MethodGet, "/api/v1/me/address", nil); anonymous.Status != http.StatusUnauthorized {
		t.Errorf("asked by nobody, the address answered %d, want 401", anonymous.Status)
	}
}
