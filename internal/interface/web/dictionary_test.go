package web_test

import (
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/interface/web"
	"github.com/dennis-dko/go-time-recording/internal/support/i18n"
)

// A language's dictionary is handed to the page as the binary carries it, at an
// address of its own, revalidated by its fingerprint; English, which has none,
// and anything that is no language are not found.
func TestADictionaryIsHandedToThePageAsTheBinaryCarriesIt(t *testing.T) {
	rec := get(t, http.MethodGet, "/i18n/de.json")
	if rec.Code != http.StatusOK {
		t.Fatalf("the German dictionary answered %d", rec.Code)
	}

	carried, _ := i18n.File("de")
	if rec.Body.String() != string(carried) {
		t.Error("the German dictionary was handed out other than the binary carries it")
	}

	if kind := rec.Header().Get("Content-Type"); !strings.HasPrefix(kind, "application/json") {
		t.Errorf("it was handed out as %q", kind)
	}

	tag := rec.Header().Get("ETag")
	if tag == "" {
		t.Fatal("it carries no fingerprint to be revalidated by")
	}

	if again := getWithHeader(t, "/i18n/de.json", "If-None-Match", tag); again.Code != http.StatusNotModified {
		t.Errorf("asked again with its fingerprint it answered %d, want 304", again.Code)
	}

	for _, path := range []string{"/i18n/en.json", "/i18n/fr.json", "/i18n/de", "/i18n/../app.js", "/i18n/"} {
		if rec := get(t, http.MethodGet, path); rec.Code != http.StatusNotFound {
			t.Errorf("%s answered %d, want 404", path, rec.Code)
		}
	}
}

// The document names the languages the page may speak, each by what it calls
// itself, so the script knows them before it has asked anything - and offline,
// where the document is what the service worker kept.
func TestTheDocumentNamesTheLanguagesItMaySpeak(t *testing.T) {
	rec := httptest.NewRecorder()
	brandedServer(web.Branding{Title: "Zeiterfassung"}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	tags := regexp.MustCompile(`<meta name="language" content="([^"]*)" data-name="([^"]*)">`).
		FindAllStringSubmatch(rec.Body.String(), -1)

	var named []string

	for _, tag := range tags {
		language, name := html.UnescapeString(tag[1]), html.UnescapeString(tag[2])
		named = append(named, language)

		if name != i18n.Name(language) {
			t.Errorf("it names %s as %q, want %q", language, name, i18n.Name(language))
		}
	}

	// In the dictionaries' order, English first, because the script's list of
	// them is read in that order and the first is the one a list falls back to.
	if !slices.Equal(named, i18n.Languages()) {
		t.Errorf("the document names %v, want %v", named, i18n.Languages())
	}
}

// REFUSAL_VALUES is read whole: every code the page writes values of, and with
// which of its functions.
func TestTheValuesOfARefusalAreReadAsThePageWritesThem(t *testing.T) {
	named := web.RefusalValues()

	js := asset(t, "/app.js")

	start := strings.Index(js, "const REFUSAL_VALUES = {")
	if start < 0 {
		t.Fatal("app.js no longer declares REFUSAL_VALUES")
	}

	block, _, _ := strings.Cut(js[start:], "\n};")
	codes := strings.Count(block, ": {")

	if codes < 1 || len(named) != codes {
		t.Fatalf("REFUSAL_VALUES has %d codes and %d were read: %v", codes, len(named), named)
	}

	if kind := named["projectClosedForBooking"][1]; kind != "statusName" {
		t.Errorf("the second value of projectClosedForBooking is read as written by %q, want statusName", kind)
	}
}
