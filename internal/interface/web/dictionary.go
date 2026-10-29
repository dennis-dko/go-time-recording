package web

import (
	"html"
	"io/fs"
	"net/http"
	"strconv"
	"strings"

	"github.com/dennis-dko/go-time-recording/internal/support/i18n"
)

// dictionaryPath is where the page fetches a language's dictionary, followed by
// the language and .json.
const dictionaryPath = "/i18n/"

// serveDictionary writes the dictionary a file under dictionaryPath names - de.json
// for German - as the binary carries it, or answers 404 for a language there is
// none of, English included, which needs none.
func serveDictionary(w http.ResponseWriter, r *http.Request, file string) {
	language, named := strings.CutSuffix(file, ".json")

	body, ok := i18n.File(language)
	if !named || !ok {
		http.NotFound(w, r)

		return
	}

	header := w.Header()
	header.Set("Content-Type", "application/json; charset=utf-8")

	// Revalidated rather than kept, like the document: a new release changes what
	// is at this address without changing the address.
	header.Set("Cache-Control", "no-cache")

	tag := `"` + fingerprint(string(body)) + `"`
	header.Set("ETag", tag)

	if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, tag) {
		w.WriteHeader(http.StatusNotModified)

		return
	}

	_, _ = w.Write(body)
}

// languagesMeta is the languages the page may speak, each by what it calls
// itself, English first - one tag a language in the document's head, where the
// script finds them before it has asked the server anything, and offline as
// well, where the document is what the service worker kept.
//
// One tag a language with its name in an attribute of its own, rather than one
// tag holding a JSON object of them all: JSON inside a quoted attribute is safe
// only as long as the escaping around it holds, and CodeQL reads it as a quote
// that could close the attribute, which is a critical alert on every pull
// request. A value escaped as text is all an attribute ever needs to hold.
func languagesMeta(languages []string, name func(string) string) string {
	tags := make([]string, 0, len(languages))

	for _, language := range languages {
		tags = append(tags, `<meta name="language" content="`+html.EscapeString(language)+
			`" data-name="`+html.EscapeString(name(language))+`">`)
	}

	return strings.Join(tags, "\n  ")
}

// RefusalValues is app.js's REFUSAL_VALUES, read rather than copied: for each
// refusal code, which of its values the page writes in words of its own, and
// with which of its functions - statusName, entityName or fmtDate. A reader that
// says refusals as the page does has to treat those values as the page does, or
// a German sentence says "completed" where the page says "abgeschlossen".
func RefusalValues() map[string]map[int]string {
	script, err := fs.ReadFile(assets, "asset/app.js")
	if err != nil {
		return map[string]map[int]string{}
	}

	return refusalValuesIn(string(script))
}

// refusalValuesIn reads REFUSAL_VALUES out of the script, a code a line:
// `  overDailyLimit: { 2: fmtDate },`.
func refusalValuesIn(script string) map[string]map[int]string {
	named := map[string]map[int]string{}

	_, after, found := strings.Cut(script, "const REFUSAL_VALUES = {")
	if !found {
		return named
	}

	body, _, _ := strings.Cut(after, "\n};")

	for _, line := range strings.Split(body, "\n") {
		code, values, found := strings.Cut(strings.TrimSpace(line), ":")
		if !found || code == "" {
			continue
		}

		written := map[int]string{}

		for _, value := range strings.Split(strings.Trim(strings.TrimSpace(values), "{},"), ",") {
			at, function, found := strings.Cut(value, ":")
			if !found {
				continue
			}

			if index, err := strconv.Atoi(strings.TrimSpace(at)); err == nil {
				written[index] = strings.TrimSpace(function)
			}
		}

		named[code] = written
	}

	return named
}
