package spreadsheet

import "strings"

// Translations of what a workbook calls things.
//
// Its own table rather than the interface's dictionary: the file is built on the
// server, in Go, and the interface's dictionary is JavaScript inside app.js.
// Sharing one would mean shipping the dictionary to the server or the sheet
// headings to the browser, and neither is worth it for two dozen words.
//
// The keys are the English words, which are also the fallback: a word nobody has
// translated is still written, in English, rather than left blank.
//
// Values are translated as well as headings - "archived" in a German export is
// not much better than "Status: archived" - and the importer accepts both
// spellings, which is what keeps a German export readable by the same
// application that wrote it.
var translations = map[string]map[string]string{
	"de": {
		// Sheet names.
		"Time entries": "Zeiteinträge",
		"Projects":     "Projekte",
		"Users":        "Benutzer",

		// Columns.
		"Date":          "Datum",
		"User":          "Benutzer",
		"Project":       "Projekt",
		"Hours":         "Stunden",
		"Description":   "Beschreibung",
		"Name":          "Name",
		"Start":         "Beginn",
		"End":           "Ende",
		"Status":        "Status",
		"Category":      "Kategorie",
		"Email":         "E-Mail",
		"Role":          "Rolle",
		"Daily target":  "Tagessoll",
		"Daily maximum": "Tagesmaximum",
		"Time zone":     "Zeitzone",
		"Directory":     "Verzeichnis",

		// Values.
		"active":    "aktiv",
		"archived":  "archiviert",
		"completed": "abgeschlossen",
		"yes":       "ja",
		"no":        "nein",
	},
}

// Translate gives the word in the language asked for, or the word itself.
//
// Exported because a preview shows the same values the file holds, and the preview
// is built by the service layer: the words a German export puts in its status
// column are the words the preview of that import has to show, or the reader is
// comparing two different vocabularies.
func Translate(language, text string) string { return translate(language, text) }

// translate gives the word in the language asked for, or the word itself.
func translate(language, text string) string {
	dictionary, ok := translations[normalise(language)]
	if !ok {
		return text
	}

	if translated, found := dictionary[text]; found {
		return translated
	}

	return text
}

// valueWords are the words that appear inside cells, as opposed to above them.
//
// Named explicitly because the reverse lookup must only consider these. The
// dictionary holds headings and sheet names too, and there "Benutzer" is the
// translation of both "User" and "Users" - harmless while writing, where the key
// is known, and ambiguous while reading, where it is not. Restricting the reverse
// direction to the words that can actually occur in a cell removes the ambiguity
// rather than relying on map order not to expose it.
var valueWords = []string{"active", "archived", "completed", "yes", "no"}

// untranslate turns a translated cell value back into the English one the
// application works in.
//
// Every language is searched, not only the one the reader is currently using: a
// file is imported by whoever has it, who may well not be the person who exported
// it, and the language a workbook was written in is not recorded anywhere in it.
// Searching all of them costs a handful of string comparisons per cell and removes
// a whole class of "it exported fine but would not import".
func untranslate(text string) string {
	trimmed := strings.TrimSpace(text)

	for _, dictionary := range translations {
		for _, english := range valueWords {
			if strings.EqualFold(dictionary[english], trimmed) {
				return english
			}
		}
	}

	return trimmed
}

// names are every name a sheet can carry, so the reader can find it whichever
// language wrote it.
func names(key string) []string {
	all := []string{key}

	for _, dictionary := range translations {
		if translated, ok := dictionary[key]; ok {
			all = append(all, translated)
		}
	}

	return all
}

// headingsIn translates a table's headings.
func headingsIn(language string, table Table) []string {
	out := make([]string, len(table.Headings))
	for i, heading := range table.Headings {
		out[i] = translate(language, heading)
	}

	return out
}

// normalise reduces "de-DE" and "DE" to "de", because that is the part that
// decides which words to use and the interface sends whatever the browser gave
// it.
func normalise(language string) string {
	lower := strings.ToLower(strings.TrimSpace(language))

	if cut := strings.IndexAny(lower, "-_"); cut > 0 {
		return lower[:cut]
	}

	return lower
}

// Languages are the languages a workbook can be written in, English aside.
//
// Exported so a test can check every one of them round-trips rather than only
// the one somebody remembered to write a case for.
func Languages() []string {
	out := make([]string, 0, len(translations))
	for language := range translations {
		out = append(out, language)
	}

	return out
}
