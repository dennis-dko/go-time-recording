package rest

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	appconfig "github.com/dennis-dko/go-time-recording/internal/infrastructure/config"
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
