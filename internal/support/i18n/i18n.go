package i18n

import (
	"embed"
	"encoding/json"
	"io/fs"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
)

// locales holds a dictionary for every language but English.
//
//go:embed locales/*.json
var locales embed.FS

// English is the language the application is written in, which has no
// dictionary.
const English = "en"

// Languages are the languages there is something to say in: English first, then
// one for each dictionary, in the order of their names.
func Languages() []string {
	languages := []string{English}

	entries, err := fs.ReadDir(locales, "locales")
	if err != nil {
		return languages
	}

	for _, entry := range entries {
		if language, ok := strings.CutSuffix(entry.Name(), ".json"); ok {
			languages = append(languages, language)
		}
	}

	slices.Sort(languages[1:])

	return languages
}

// File is a language's dictionary as it is stored, for a browser to read as it
// is; false for English, and for anything that names no dictionary - a request
// for one is somebody's address bar, so what it says is checked before it is
// looked for.
func File(language string) ([]byte, bool) {
	if language == English || language == "" || strings.Trim(language, "abcdefghijklmnopqrstuvwxyz-") != "" {
		return nil, false
	}

	body, err := fs.ReadFile(locales, "locales/"+language+".json")

	return body, err == nil
}

// Dictionary is a language's dictionary: empty for English and for a language
// there is none of, so a lookup in it falls back to the English it carries. A
// file that does not read is a broken build, which the tests stop before it
// ships.
func Dictionary(language string) map[string]string {
	words := map[string]string{}

	if body, ok := File(language); ok {
		_ = json.Unmarshal(body, &words)
	}

	return words
}

// Name is what a language calls itself, which is what somebody looking for
// their own language looks for: its dictionary's language.name, and English for
// English.
func Name(language string) string {
	if language == English {
		return "English"
	}

	if name := Dictionary(language)["language.name"]; name != "" {
		return name
	}

	return language
}

// Fill puts values into a sentence where it says {0}, {1} ..., as the web
// interface's fillIn does; a place there is no value for is left as it stands.
func Fill(sentence string, values ...string) string {
	if len(values) == 0 {
		return sentence
	}

	var out strings.Builder

	for rest := sentence; rest != ""; {
		before, after, found := strings.Cut(rest, "{")
		out.WriteString(before)

		if !found {
			break
		}

		digits, tail, closed := strings.Cut(after, "}")

		at, err := strconv.Atoi(digits)
		if !closed || err != nil || at < 0 || at >= len(values) {
			out.WriteString("{")

			rest = after

			continue
		}

		out.WriteString(values[at])

		rest = tail
	}

	return out.String()
}

// Translator says things in one language.
type Translator struct {
	language string
	words    map[string]string
}

// For is a Translator for a language. One there is no dictionary for says
// everything in English.
func For(language string) Translator {
	return Translator{language: language, words: Dictionary(language)}
}

// Language is the language it says things in.
func (t Translator) Language() string { return t.language }

// Text is what a key says in this language, or english where the dictionary
// has nothing for it, with values put in where it says {0}, {1} ...
func (t Translator) Text(key, english string, values ...string) string {
	sentence, ok := t.words[key]
	if !ok {
		sentence = english
	}

	return Fill(sentence, values...)
}

// Lookup is what a key says in this language, and false where the dictionary
// has nothing for it - for a caller with an English sentence of its own to fall
// back on, such as an installation's own words for a refusal.
func (t Translator) Lookup(key string) (string, bool) {
	sentence, ok := t.words[key]

	return sentence, ok
}

// Number writes a figure to two places, the way the web interface writes one in
// this language: 1,234.50 in English and 1.234,50 in German - with the point
// and the grouping a dictionary names as number.point and number.group.
func (t Translator) Number(v float64) string {
	point := t.Text("number.point", ".")
	group := t.Text("number.group", ",")

	whole, fraction, _ := strings.Cut(strconv.FormatFloat(math.Abs(v), 'f', 2, 64), ".")

	var grouped strings.Builder

	for i, digit := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			grouped.WriteString(group)
		}

		grouped.WriteRune(digit)
	}

	sign := ""
	if v < 0 && (whole != "0" || fraction != "00") {
		sign = "-"
	}

	return sign + grouped.String() + point + fraction
}

// Day writes a calendar day the way this language writes one: 2026-08-27 in
// English and 27.08.2026 in German - in the Go layout a dictionary names as
// layout.day.
func (t Translator) Day(day time.Time) string {
	return day.Format(t.Text("layout.day", time.DateOnly))
}

// Moment writes a day and the time on it, for a line saying when something was
// made: the day as Day writes it, then the time in the layout a dictionary names
// as layout.time.
func (t Translator) Moment(at time.Time) string {
	return t.Day(at) + " " + at.Format(t.Text("layout.time", "15:04"))
}
