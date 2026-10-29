//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/support/qrcode"
)

// A new calendar address comes with a QR code of itself, for the phone a
// calendar is usually on: the address exactly as the page shows it - the page's
// own origin and the path - and only in the answer that made it, like the key it
// carries.
func TestANewCalendarAddressComesWithACodeOfItself(t *testing.T) {
	t.Parallel()

	a, _, worker := startWithWorker(t)

	var made struct {
		Path string `json:"path"`
		QR   string `json:"qr"`
	}

	worker.must(worker.api(http.MethodPost, "/me/calendar-feed", nil),
		http.StatusCreated, http.StatusOK).Data(t, &made)

	if made.Path == "" {
		t.Fatal("the new address came without its path, so there is nothing to compare its code with")
	}

	if want, err := qrcode.SVGDataURI(a.BaseURL() + made.Path); err != nil || made.QR != want {
		t.Errorf("the code that came with the address is not the code of %s%s (%d characters, want %d)",
			a.BaseURL(), made.Path, len(made.QR), len(want))
	}

	var state map[string]any

	worker.must(worker.api(http.MethodGet, "/me/calendar-feed", nil), http.StatusOK).Data(t, &state)

	if _, shown := state["qr"]; shown {
		t.Error("the feed's state shows its code, which exists only in the answer that made the address")
	}
}
