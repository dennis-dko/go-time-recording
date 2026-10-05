package rest

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"
	gofrHTTP "gofr.dev/pkg/gofr/http"
	"gofr.dev/pkg/gofr/logging"

	appconfig "github.com/dennis-dko/go-time-recording/internal/infrastructure/config"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// A restart into a database that does not answer is refused before anything is
// promised.
//
// Saving a connection does not try it, and the next start probes it before
// anything else and ends on one that does not answer - so a typo saved and then
// restarted into was an installation that did not come back, with nothing left
// in a browser to put it right.
//
// Only the refusal is asked here, and on purpose: a restart that went ahead would
// replace this test process with a fresh copy of itself where execve exists.
// Not parallel, because the stored connection is read from the working directory.
func TestARestartIntoADatabaseThatDoesNotAnswerIsRefused(t *testing.T) {
	t.Chdir(t.TempDir())

	// Nothing listens on port 1, so the probe is refused at once.
	stored := appconfig.Datasource{
		Dialect: "postgres", Name: "gtr", Host: "127.0.0.1", Port: "1",
		User: "gtr", Password: "a-password", SSLMode: "disable",
	}

	if err := appconfig.SaveDatasource(appconfig.DatasourceFile, stored); err != nil {
		t.Fatal(err)
	}

	running := appconfig.Datasource{Dialect: "sqlite", Name: "gtr.db"}
	h := NewRestartHandler(nil, NewAuthorizer(false), appconfig.Config{}, running, false, appconfig.Telemetry{})

	r := httptest.NewRequest(http.MethodPost, "/api/v1/settings/restart", nil)
	c := &gofr.Context{
		Context:   context.Background(),
		Request:   gofrHTTP.NewRequest(r),
		Container: &container.Container{Logger: logging.NewMockLogger(logging.ERROR)},
	}

	_, err := h.Restart(c)

	var coded interface {
		StatusCode() int
		Response() map[string]any
	}

	if !errors.As(err, &coded) {
		t.Fatalf("a restart into a database that does not answer was answered %v", err)
	}

	body := coded.Response()

	if body["code"] != "restartWouldNotStart" || coded.StatusCode() != http.StatusConflict {
		t.Errorf("answered %d %v, want %d restartWouldNotStart", coded.StatusCode(), body["code"], http.StatusConflict)
	}

	if detail, _ := body["detail"].(string); detail == "" {
		t.Error("the refusal does not carry what the driver said")
	}

	// Asked of apperror too, so the code is one the translation tests can see.
	if apperror.KindOf(err) == apperror.KindInternal && body["code"] == nil {
		t.Error("the refusal reached the client as an internal error")
	}
}
