package rest

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/infrastructure/selfupdate"
	"github.com/dennis-dko/go-time-recording/internal/support/hosting"
)

// A newer release that published nothing for this platform says so.
//
// The button is offered only for a release with a build for this platform, so
// for one without it the button went away - while the card went on saying the
// version was available and describing the download it would check and the
// restart that would follow. Somebody looking for the button it described found
// none, and nothing said why. It happens on any platform the release does not
// build for, and to a release whose checksums were never published.
func TestANewerReleaseWithNothingForThisPlatformSaysSo(t *testing.T) {
	if hosting.InContainer() {
		t.Skip("in a container the card first says the binary is not what changes")
	}

	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"tag_name":"v9.9.9","body":"notes","html_url":"https://example.com/r",
			"assets":[{"name":"go-time-recording_v9.9.9_plan9_386","browser_download_url":"https://example.com/b"},
			{"name":"SHA256SUMS","browser_download_url":"https://example.com/s"}]}`)
	}))
	defer feed.Close()

	h := NewUpdateHandler(nil, selfupdate.New(feed.URL, ""), nil, "v1.0.0", true)

	card := h.describe(requestContext())

	if !card.Newer || card.Available {
		t.Fatalf("the card says newer=%v available=%v; this case needs a newer release that "+
			"cannot be installed", card.Newer, card.Available)
	}

	if card.Why != "noBinary" {
		t.Errorf("a newer release with nothing for this platform is not offered, and the card "+
			"is told %q as the reason", card.Why)
	}
}
