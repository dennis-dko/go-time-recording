package rest

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/infrastructure/announce"
	appconfig "github.com/dennis-dko/go-time-recording/internal/infrastructure/config"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/selfupdate"
	"github.com/dennis-dko/go-time-recording/internal/support/hosting"
)

// fallingTransport falls over the moment it is asked for anything.
type fallingTransport struct{}

func (fallingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	panic("the download fell over")
}

// An install that panics takes back the "installing" it raised.
//
// GoFr recovers a panic inside a handler, so the process carries on - and it
// carried on with "installing" on every screen, and on every screen that
// connected afterwards, for an install that had ended: the notice was taken
// back on the error path only. A panic there has been real once already, when
// the sentence reporting a checksum mismatch sliced past the end of a short one.
//
// Not parallel, because the stored connection is read from the working directory.
func TestAnInstallThatPanicsTakesItsAnnouncementBack(t *testing.T) {
	if hosting.InContainer() {
		t.Skip("in a container the binary is not what an update replaces")
	}

	t.Chdir(t.TempDir())

	asset := fmt.Sprintf("go-time-recording_v9.9.9_%s_%s%s", runtime.GOOS, runtime.GOARCH, binarySuffix())

	var feed *httptest.Server

	feed = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			_, _ = fmt.Fprintf(w, `{"tag_name":"v9.9.9","assets":[
				{"name":%q,"browser_download_url":"%s/binary"},
				{"name":"SHA256SUMS","browser_download_url":"%s/sums"}]}`, asset, feed.URL, feed.URL)
		case "/sums":
			_, _ = fmt.Fprintf(w, "%s  %s\n", strings.Repeat("0", 64), asset)
		default:
			http.NotFound(w, r)
		}
	}))
	defer feed.Close()

	source := selfupdate.New(feed.URL, "")
	source.Downloader = &http.Client{Transport: fallingTransport{}}

	hub := announce.New()
	h := NewUpdateHandler(NewAuthorizer(false), source, hub, "v1.0.0", true).
		WithConnection(appconfig.Datasource{Dialect: "sqlite", Name: "gtr.db"})

	fell := func() (fell any) {
		defer func() { fell = recover() }()

		_, _ = h.Apply(requestContext())

		return nil
	}()

	if fell != "the download fell over" {
		t.Fatalf("the install did not fall over where this case makes it fall: %v", fell)
	}

	if last, standing := hub.Last(); standing {
		t.Errorf("%q is still announced to every screen for an install that fell over", last.Kind)
	}
}
