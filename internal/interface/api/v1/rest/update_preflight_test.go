package rest

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/infrastructure/announce"
	appconfig "github.com/dennis-dko/go-time-recording/internal/infrastructure/config"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/selfupdate"
	"github.com/dennis-dko/go-time-recording/internal/support/hosting"
)

// An image update into a database that does not answer is refused before
// anything is announced or asked of the updater.
//
// The updater recreates this container and removes the image it replaced, with
// no look at whether the new one comes up - and the new one opens the stored
// connection first and ends on one that does not answer, so it would go round
// its restart policy for good with the old image already gone. The restart
// button asks the same question for the same reason.
//
// Not parallel, because the stored connection is read from the working directory.
func TestAnImageUpdateIntoADatabaseThatDoesNotAnswerIsRefused(t *testing.T) {
	t.Chdir(t.TempDir())

	stored := appconfig.Datasource{
		Dialect: "postgres", Name: "gtr", Host: "127.0.0.1", Port: "1",
		User: "gtr", Password: "a-password", SSLMode: "disable",
	}

	if err := appconfig.SaveDatasource(appconfig.DatasourceFile, stored); err != nil {
		t.Fatal(err)
	}

	h, hub, dir := imageUpdateUnderTest(t)

	_, err := h.askForTheImage(requestContext(), "v9.9.9")

	var coded interface {
		StatusCode() int
		Response() map[string]any
	}

	if !errors.As(err, &coded) || coded.StatusCode() != http.StatusConflict ||
		coded.Response()["code"] != "restartWouldNotStart" {
		t.Fatalf("an image update into a database that does not answer was answered %v", err)
	}

	if last, standing := hub.Last(); standing {
		t.Errorf("%q was announced to every screen for an update that was refused", last.Kind)
	}

	if _, statErr := os.Stat(filepath.Join(dir, "request")); statErr == nil {
		t.Error("the updater was asked to replace the container anyway")
	}
}

// A binary update into a database that does not answer is refused before it is
// announced, as one by image is.
//
// The binary path tells every screen that a restart is coming once the download
// is in place, and the page then asks for the restart - which is refused while
// the next start's database does not answer. The announcement then stood on
// every screen, and the hub handed it to each one that connected afterwards.
//
// Not parallel, because the stored connection is read from the working directory.
func TestABinaryUpdateIntoADatabaseThatDoesNotAnswerIsRefusedBeforeItIsAnnounced(t *testing.T) {
	if hosting.InContainer() {
		t.Skip("in a container the binary is not what an update replaces")
	}

	t.Chdir(t.TempDir())

	stored := appconfig.Datasource{
		Dialect: "postgres", Name: "gtr", Host: "127.0.0.1", Port: "1",
		User: "gtr", Password: "a-password", SSLMode: "disable",
	}

	if err := appconfig.SaveDatasource(appconfig.DatasourceFile, stored); err != nil {
		t.Fatal(err)
	}

	var feed *httptest.Server

	feed = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)

			return
		}

		_, _ = fmt.Fprintf(w, `{"tag_name":"v9.9.9","assets":[
			{"name":"go-time-recording_v9.9.9_%s_%s%s","browser_download_url":"%s/binary"},
			{"name":"SHA256SUMS","browser_download_url":"%s/sums"}]}`,
			runtime.GOOS, runtime.GOARCH, binarySuffix(), feed.URL, feed.URL)
	}))
	defer feed.Close()

	hub := announce.New()
	h := NewUpdateHandler(NewAuthorizer(false), selfupdate.New(feed.URL, ""), hub, "v1.0.0", true).
		WithConnection(appconfig.Datasource{Dialect: "sqlite", Name: "gtr.db"})

	_, err := h.Apply(requestContext())

	var coded interface {
		StatusCode() int
		Response() map[string]any
	}

	if !errors.As(err, &coded) || coded.Response()["code"] != "restartWouldNotStart" {
		t.Fatalf("a binary update into a database that does not answer was answered %v", err)
	}

	if last, standing := hub.Last(); standing || last.Kind != "" {
		t.Errorf("%q was announced to every screen for an update that was refused", last.Kind)
	}
}

// binarySuffix is what a release names this platform's binary with at the end.
func binarySuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}

	return ""
}
