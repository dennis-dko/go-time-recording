package web_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// getWithHeader is the one thing the shared helper cannot do: ask again while
// carrying what the browser already has.
func getWithHeader(t *testing.T, path, name, value string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set(name, value)

	rec := httptest.NewRecorder()
	newServer().ServeHTTP(rec, req)

	return rec
}

// The interface is not re-sent to a browser that already has it.
//
// Every page load used to fetch app.js and app.css in full - six thousand lines of
// script and a stylesheet, on every reload, for files that cannot change while the
// process runs. http.FileServer would normally have prevented that by itself, but
// it works from the modification time, and a file compiled into the binary has
// none: embed reports the zero time, so no Last-Modified and no ETag ever went out
// and there was nothing for a browser to revalidate against.

func TestAssetsCarryAnETagAndRevalidate(t *testing.T) {
	first := get(t, http.MethodGet, "/app.js")

	if first.Code != http.StatusOK {
		t.Fatalf("expected 200 for /app.js, got %d", first.Code)
	}

	tag := first.Header().Get("ETag")
	if tag == "" {
		t.Fatal("/app.js carries no ETag, so a browser has nothing to revalidate with")
	}

	if got := first.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("/app.js is sent with Cache-Control %q, want no-cache", got)
	}

	// Asset URLs are not fingerprinted - /app.js is /app.js in every release - so
	// anything that lets a browser keep one without asking would leave it running
	// an old interface against a new API.
	if got := first.Header().Get("Cache-Control"); got == "immutable" {
		t.Error("an unfingerprinted asset must not be cached without revalidation")
	}
}

func TestARepeatRequestWithTheTagGets304AndNoBody(t *testing.T) {
	first := get(t, http.MethodGet, "/app.js")
	tag := first.Header().Get("ETag")

	again := getWithHeader(t, "/app.js", "If-None-Match", tag)

	if again.Code != http.StatusNotModified {
		t.Fatalf("a repeat request with the tag answered %d, want 304", again.Code)
	}

	if again.Body.Len() != 0 {
		t.Errorf("a 304 carried %d bytes of body", again.Body.Len())
	}
}

// A tag from a different build is not honoured, or an upgrade would leave every
// browser holding the previous release's interface.
func TestAStaleTagIsNotHonoured(t *testing.T) {
	again := getWithHeader(t, "/app.js", "If-None-Match", `"not-the-current-build"`)

	if again.Code != http.StatusOK {
		t.Fatalf("a stale tag answered %d, want the file", again.Code)
	}

	if again.Body.Len() == 0 {
		t.Error("a stale tag was answered with an empty body")
	}
}

// Two different files do not share a tag, which is what a length-based or
// path-based scheme would get wrong.
func TestDifferentAssetsHaveDifferentTags(t *testing.T) {
	js := get(t, http.MethodGet, "/app.js").Header().Get("ETag")
	css := get(t, http.MethodGet, "/app.css").Header().Get("ETag")

	if js == "" || css == "" {
		t.Fatal("one of the assets carries no tag")
	}

	if js == css {
		t.Errorf("app.js and app.css share the tag %s", js)
	}
}

// The API is left alone. Its answers depend on who is asking and change between
// two requests a second apart, so a validator on them would be wrong the moment
// it was written.
func TestTheAPIIsNotGivenAValidator(t *testing.T) {
	rec := get(t, http.MethodGet, "/api/v1/me")

	if rec.Header().Get("ETag") != "" {
		t.Error("an API path was given an ETag")
	}
}
