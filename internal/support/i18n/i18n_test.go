package i18n_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/support/i18n"
)

// Every dictionary is written the one way - keys in order, two spaces, nothing
// escaped that need not be - so that two people adding a sentence at once meet
// as two lines in a merge rather than as a file rewritten twice.
func TestEveryDictionaryIsWrittenTheOneWay(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("locale", "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("found no dictionaries: %v", err)
	}

	for _, file := range files {
		stored, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}

		var words map[string]string
		if err := json.Unmarshal(stored, &words); err != nil {
			t.Errorf("%s does not read as one object of sentences: %v", file, err)

			continue
		}

		var canonical bytes.Buffer

		encoder := json.NewEncoder(&canonical)
		encoder.SetEscapeHTML(false)
		encoder.SetIndent("", "  ")

		if err := encoder.Encode(words); err != nil {
			t.Fatal(err)
		}

		if !bytes.Equal(stored, canonical.Bytes()) {
			t.Errorf("%s is not written the one way: keys in order, two spaces, nothing escaped "+
				"that need not be, one line break at the end", file)
		}
	}
}

// Every dictionary has every key the German one has, and no other: the German
// is the complete one, which a new language starts from, and a key one language
// lacks is a sentence that reader gets in English.
func TestEveryDictionaryHasEveryKey(t *testing.T) {
	german := i18n.Dictionary("de")
	if len(german) < 1000 {
		t.Fatalf("the German dictionary has %d sentences; it is not being read", len(german))
	}

	for _, language := range i18n.Languages()[1:] {
		words := i18n.Dictionary(language)

		for key := range german {
			if _, ok := words[key]; !ok {
				t.Errorf("%s has no %s", language, key)
			}
		}

		for key := range words {
			if _, ok := german[key]; !ok {
				t.Errorf("%s has %s, which German does not", language, key)
			}
		}
	}
}

// The languages offered are the languages there are dictionaries for: a
// language somebody may choose and nothing translates, or a dictionary nobody
// may choose, is a file or a list that was changed without the other.
func TestTheLanguagesOfferedAreTheLanguagesThereAreWordsFor(t *testing.T) {
	offered := slices.Clone(model.SupportedLanguages())
	sort.Strings(offered[1:])

	if languages := i18n.Languages(); !slices.Equal(languages, offered) {
		t.Errorf("there are words for %v and model.SupportedLanguages offers %v", languages, model.SupportedLanguages())
	}

	if languages := i18n.Languages(); languages[0] != i18n.English || model.SupportedLanguages()[0] != i18n.English {
		t.Errorf("English is not first: the fallback goes first, so a list read in order falls back to it")
	}
}

// Every language writes its figures its own way - this package's number.* - and
// a figure is written to two places, grouped, with the sign where it belongs.
func TestEveryLanguageWritesItsFigures(t *testing.T) {
	for _, language := range i18n.Languages()[1:] {
		words := i18n.Dictionary(language)

		if words["number.point"] == "" || words["number.group"] == "" {
			t.Errorf("%s does not say how it writes a figure: number.point %q, number.group %q",
				language, words["number.point"], words["number.group"])
		}
	}

	for _, tc := range []struct {
		language string
		value    float64
		want     string
	}{
		{"en", 1234567.891, "1,234,567.89"},
		{"de", 1234.5, "1.234,50"},
		{"de", -1.5, "-1,50"},
		{"de", -0.004, "0,00"},
		{"en", 0, "0.00"},
	} {
		if got := i18n.For(tc.language).Number(tc.value); got != tc.want {
			t.Errorf("%v in %s is %q, want %q", tc.value, tc.language, got, tc.want)
		}
	}
}

// Every language writes a day and a time its own way - this package's layout.*,
// each a Go layout - and a layout is one only if what it writes can be read back
// as what was written: a translator writing TT.MM.JJJJ, the way the date fields
// say it, gets those letters back on every page, and a day written without its
// year is not the day it was.
func TestEveryLanguageWritesItsDays(t *testing.T) {
	at := time.Date(2026, time.August, 27, 23, 40, 0, 0, time.UTC)

	for _, language := range i18n.Languages() {
		words := i18n.Dictionary(language)

		for key, part := range map[string]struct{ name, english string }{
			"layout.day":  {"day", time.DateOnly},
			"layout.time": {"time", "15:04"},
		} {
			if language != i18n.English && words[key] == "" {
				t.Errorf("%s does not say how it writes a %s: %s is empty", language, part.name, key)

				continue
			}

			layout := i18n.For(language).Text(key, part.english)

			back, err := time.Parse(layout, at.Format(layout))
			if err != nil || back.Format(part.english) != at.Format(part.english) {
				t.Errorf("%s writes a %s as %q, which reads back as %q: %v", language, part.name,
					at.Format(layout), back.Format(part.english), err)
			}
		}
	}

	for _, tc := range []struct {
		language string
		day      string
		moment   string
	}{
		{"en", "2026-08-27", "2026-08-27 23:40"},
		{"de", "27.08.2026", "27.08.2026 23:40"},
		{"", "2026-08-27", "2026-08-27 23:40"},
	} {
		words := i18n.For(tc.language)

		if got := words.Day(at); got != tc.day {
			t.Errorf("a day in %q is %q, want %q", tc.language, got, tc.day)
		}

		if got := words.Moment(at); got != tc.moment {
			t.Errorf("a moment in %q is %q, want %q", tc.language, got, tc.moment)
		}
	}
}

// Every language says what it calls itself, which is how somebody finds theirs
// in a list of them.
func TestEveryLanguageNamesItself(t *testing.T) {
	for _, language := range i18n.Languages() {
		if name := i18n.Name(language); name == language || name == "" {
			t.Errorf("%s calls itself %q", language, name)
		}
	}

	if i18n.Name("de") != "Deutsch" || i18n.Name("en") != "English" {
		t.Errorf("German and English call themselves %q and %q", i18n.Name("de"), i18n.Name("en"))
	}
}

// A dictionary is handed out by its language's name and by nothing else: the
// name comes from an address somebody typed.
func TestOnlyADictionaryIsHandedOut(t *testing.T) {
	if _, ok := i18n.File("de"); !ok {
		t.Error("the German dictionary is not handed out")
	}

	for _, name := range []string{"en", "", "fr", "../i18n", "locale/de", "de.json", "DE", "de/../de"} {
		if _, ok := i18n.File(name); ok {
			t.Errorf("%q was handed out as a dictionary", name)
		}
	}
}

// Values go where a sentence says {0}, {1} ..., and a place there is no value for
// is left as it stands.
func TestValuesGoWhereTheSentenceSays(t *testing.T) {
	for _, tc := range []struct {
		sentence string
		values   []string
		want     string
	}{
		{"{1} before {0}", []string{"a", "b"}, "b before a"},
		{"{0} and {0}", []string{"x"}, "x and x"},
		{"{0} and {3}", []string{"x"}, "x and {3}"},
		{"{ not {0", []string{"x"}, "{ not {0"},
		{"{name}", []string{"x"}, "{name}"},
		{"nothing to fill", nil, "nothing to fill"},
	} {
		if got := i18n.Fill(tc.sentence, tc.values...); got != tc.want {
			t.Errorf("%q with %q is %q, want %q", tc.sentence, tc.values, got, tc.want)
		}
	}

	german := i18n.For("de")
	if got := german.Text("language.name", "English"); got != "Deutsch" {
		t.Errorf("German says language.name as %q", got)
	}

	if got := i18n.For("en").Text("language.name", "English"); got != "English" {
		t.Errorf("English says language.name as %q", got)
	}

	if got := german.Text("no.such.key", "{0} in English", "it"); got != "it in English" {
		t.Errorf("a key German lacks says %q", got)
	}
}
