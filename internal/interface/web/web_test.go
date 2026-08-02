package web_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/interface/web"
)

// nextHandler stands in for the API routes behind the UI middleware.
const nextMarker = "API REACHED"

func newServer() http.Handler {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(nextMarker))
	})

	return web.Handler()(next)
}

func get(t *testing.T, method, path string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	newServer().ServeHTTP(rec, httptest.NewRequest(method, path, nil))

	return rec
}

func TestServesEmbeddedIndex(t *testing.T) {
	rec := get(t, http.MethodGet, "/")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	if !strings.Contains(rec.Body.String(), "<title>Time Recording</title>") {
		t.Error("expected the embedded index.html")
	}
}

func TestServesEmbeddedAssets(t *testing.T) {
	for _, path := range []string{"/app.css", "/app.js", "/theme.js"} {
		t.Run(path, func(t *testing.T) {
			rec := get(t, http.MethodGet, path)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200 for %s, got %d", path, rec.Code)
			}

			if rec.Body.Len() == 0 {
				t.Errorf("%s served empty", path)
			}
		})
	}
}

// The API must never be shadowed by the file server, even for paths that do
// not exist as files.
func TestAPIPathsPassThrough(t *testing.T) {
	paths := []string{
		"/api/v1/users",
		"/.well-known/alive",
		"/.well-known/health",
		"/metrics",
		"/favicon.ico",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			rec := get(t, http.MethodGet, path)

			if rec.Body.String() != nextMarker {
				t.Errorf("%s should reach the API, got %q", path, rec.Body.String())
			}
		})
	}
}

// Non-GET requests belong to the API even on UI-looking paths, otherwise a
// POST would be answered with HTML.
func TestNonGetRequestsPassThrough(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			rec := get(t, method, "/")

			if rec.Body.String() != nextMarker {
				t.Errorf("%s / should reach the API, got %q", method, rec.Body.String())
			}
		})
	}
}

// A deep link must render the app rather than 404, so a browser reload on a
// client-side route still works.
func TestUnknownUIPathFallsBackToIndex(t *testing.T) {
	rec := get(t, http.MethodGet, "/projects/42")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	if !strings.Contains(rec.Body.String(), "<title>Time Recording</title>") {
		t.Error("expected the SPA fallback to serve index.html")
	}
}
