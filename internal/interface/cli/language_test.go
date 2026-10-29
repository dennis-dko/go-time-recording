package cli

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/dennis-dko/go-time-recording/internal/interface/web"
	"github.com/dennis-dko/go-time-recording/internal/support/i18n"
)

// The language is what was asked for on the line, then in GTR_LANG, then what
// the environment and the system are set to, then English - and a language that
// was asked for and is not had is refused, in the language track falls back to.
func TestTheLanguageIsChosenInTheOrderItWasAskedFor(t *testing.T) {
	for _, tc := range []struct {
		name     string
		flag     string
		env      map[string]string
		system   string
		code     string
		explicit bool
		refused  string
	}{
		{name: "nothing set anywhere", code: "en"},
		{name: "the flag", flag: "de", code: "de", explicit: true},
		{name: "the flag over GTR_LANG", flag: "en", env: map[string]string{"GTR_LANG": "de"}, code: "en", explicit: true},
		{name: "GTR_LANG", env: map[string]string{"GTR_LANG": "de"}, code: "de", explicit: true},
		{name: "GTR_LANG as a locale", env: map[string]string{"GTR_LANG": "de_DE.UTF-8"}, code: "de", explicit: true},
		{name: "GTR_LANG over LANG", env: map[string]string{"GTR_LANG": "en-GB", "LANG": "de_DE.UTF-8"}, code: "en", explicit: true},
		{name: "LANG", env: map[string]string{"LANG": "de_DE.UTF-8"}, code: "de"},
		{name: "LC_ALL over LANG", env: map[string]string{"LC_ALL": "en_US.UTF-8", "LANG": "de_DE.UTF-8"}, code: "en"},
		{name: "LC_MESSAGES over LANG", env: map[string]string{"LC_MESSAGES": "de_AT", "LANG": "en_US"}, code: "de"},
		{name: "the environment over the system", env: map[string]string{"LANG": "C.UTF-8"}, system: "de-DE", code: "en"},
		{name: "the system", system: "de-DE", code: "de"},
		{name: "a system in a language there are no words for", system: "fr-FR", code: "en"},
		{name: "a LANG in a language there are no words for", env: map[string]string{"LANG": "fr_FR.UTF-8"}, code: "en"},
		{name: "a LANG that only starts with the letters", env: map[string]string{"LANG": "default"}, code: "en"},
		{name: "a flag naming one there are no words for", flag: "fr", env: map[string]string{"LANG": "de_DE"},
			code: "de", refused: `--lang nimmt en, de, nicht „fr“`},
		{name: "a flag naming one there are no words for, under a GTR_LANG there are", flag: "fr",
			env: map[string]string{"GTR_LANG": "de"}, code: "de", explicit: true, refused: `--lang nimmt en, de, nicht „fr“`},
		{name: "GTR_LANG naming one there are no words for", env: map[string]string{"GTR_LANG": "english"},
			code: "en", refused: `GTR_LANG takes en, de, not "english"`},
		{name: "a stray GTR_LANG under a flag there are words for", flag: "de", env: map[string]string{"GTR_LANG": "english"},
			code: "de", explicit: true},
	} {
		env := func(key string) string { return tc.env[key] }
		asked := 0

		chosen, explicit, err := chooseLanguage(tc.flag, env, func() string { asked++; return tc.system })

		if tc.env["LANG"] != "" && asked > 0 {
			t.Errorf("%s: the system was asked although LANG says %q", tc.name, tc.env["LANG"])
		}

		if chosen.code() != tc.code || explicit != tc.explicit {
			t.Errorf("%s: chose %s (asked for: %v), want %s (%v)", tc.name, chosen.code(), explicit, tc.code, tc.explicit)
		}

		switch {
		case tc.refused == "" && err != nil:
			t.Errorf("%s: refused with %v", tc.name, err)
		case tc.refused != "" && (err == nil || err.Error() != tc.refused):
			t.Errorf("%s: answered %v, want %q", tc.name, err, tc.refused)
		}
	}
}

// --lang is taken from wherever it stands on the line, except as the text of -m
// or the day of -d, and with nothing after it the line cannot be read.
func TestTheLanguageIsTakenFromWhereverItStandsOnTheLine(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		flag  string
		rest  []string
		named bool
	}{
		{[]string{"--lang", "de", "status"}, "de", []string{"status"}, true},
		{[]string{"status", "--lang=en"}, "en", []string{"status"}, true},
		{[]string{"book", "1", "Website", "--lang", "de", "-d", "2026-10-21"}, "de", []string{"book", "1", "Website", "-d", "2026-10-21"}, true},
		{[]string{"start", "Website", "-m", "--lang"}, "", []string{"start", "Website", "-m", "--lang"}, true},
		{[]string{"status", "--lang"}, "", []string{"status"}, false},
	} {
		flag, rest, named := languageFlag(tc.args)
		if flag != tc.flag || !slices.Equal(rest, tc.rest) || named != tc.named {
			t.Errorf("%q read as %q %q %v, want %q %q %v", tc.args, flag, rest, named, tc.flag, tc.rest, tc.named)
		}
	}
}

// A command speaks German when asked to, with the hours, the days and the
// project that is none written the way the German web interface writes them.
func TestTrackSpeaksGermanWhenAsked(t *testing.T) {
	f, server := newFakeInstallation(t)

	for _, tc := range []struct {
		args []string
		says []string
	}{
		// 07:12 UTC is 09:12 in Berlin in October.
		{[]string{"--lang", "de", "status"}, []string{"Läuft seit 09:12 auf Consulting – bisher 1:30.", "Heute 3,00 Std. gebucht."}},
		{[]string{"book", "1:30", "consult", "-d", "2026-10-21", "--lang=de"}, []string{"1,50 Std. auf Consulting für den 21.10.2026 gebucht."}},
		{[]string{"today", "--lang", "de"}, []string{"   2,50 Std.  Consulting - review", "   0,50 Std.  kein Projekt", "   3,00 Std.  insgesamt"}},
		{[]string{"help", "--lang", "de"}, []string{"Zeit im Terminal buchen", "--lang <Sprache>"}},
	} {
		status, out, errOut := track(t, server.URL, f.token, tc.args...)
		if status != 0 {
			t.Errorf("%q answered %d: %s", tc.args, status, errOut)

			continue
		}

		for _, said := range tc.says {
			if !strings.Contains(out, said) {
				t.Errorf("%q said %q, want %q in it", tc.args, out, said)
			}
		}

		// An hour ends on the point of "Std.", and in German that point is also
		// the point of a sentence ending on one - so a sentence that put its own
		// after it would end twice.
		if strings.Contains(out, "..") {
			t.Errorf("%q said %q, which ends a sentence twice", tc.args, out)
		}
	}

	// And what cannot be read, in German too - from GTR_LANG this time.
	env := map[string]string{"GTR_URL": server.URL, "GTR_TOKEN": f.token, "GTR_LANG": "de"}
	if status, _, errOut := trackWith(t, env, "book", "viel"); status != 2 || !strings.Contains(errOut, "„viel“ ist keine Stundenzahl") {
		t.Errorf("unreadable hours answered %d %q, want 2 in German", status, errOut)
	}

	env["GTR_TOKEN"] = "gtr_wrong"
	if status, _, errOut := trackWith(t, env, "status"); status != 1 || !strings.Contains(errOut, "Mein Konto, API-Tokens") {
		t.Errorf("an unknown token answered %d %q, want 1 in German", status, errOut)
	}
}

// Where nobody asked for a language, track speaks the one the account chose in
// the browser - over the one the system is set to, and under one asked for.
func TestTrackSpeaksTheLanguageTheAccountChose(t *testing.T) {
	f, server := newFakeInstallation(t)
	f.language = "de"

	env := map[string]string{"GTR_URL": server.URL, "GTR_TOKEN": f.token, "LANG": "en_US.UTF-8"}

	if status, out, errOut := trackWith(t, env, "status"); status != 0 || !strings.Contains(out, "Heute 3,00 Std. gebucht.") {
		t.Errorf("an account that chose German, on an English system, answered %d %q %q", status, out, errOut)
	}

	if status, out, _ := trackWith(t, env, "status", "--lang", "en"); status != 0 || !strings.Contains(out, "Booked today: 3.00 h.") {
		t.Errorf("--lang en under an account that chose German answered %d %q", status, out)
	}

	f.mu.Lock()
	f.language = ""
	f.mu.Unlock()

	if status, out, _ := trackWith(t, env, "status"); status != 0 || !strings.Contains(out, "Booked today: 3.00 h.") {
		t.Errorf("an account that chose nothing, on an English system, answered %d %q", status, out)
	}
}

// A refusal is said in the browser's German: the sentence the web interface has
// for its code, with a status, a kind of record and a day written as the page
// writes them, the fields named as its forms name them - and the installation's
// own English where the page has no sentence for it.
func TestARefusalIsSaidInTheBrowsersGerman(t *testing.T) {
	l := speaking("de")

	for _, tc := range []struct {
		name   string
		status int
		body   string
		says   string
	}{
		{"a status among the values", 409,
			`{"error":{"code":"projectClosedForBooking","message":"closed","values":["Website","completed"]}}`,
			"Projekt „Website“ ist abgeschlossen und nimmt keine Zeiteinträge mehr an."},
		{"a day and fractions among the values", 422,
			`{"error":{"code":"overDailyLimit","message":"over","values":[2.5,10.5,"2026-10-21",10]}}`,
			"2,50 Std. würden am 21.10.2026 zusammen 10,50 Std. ergeben und damit das Tagesmaximum von 10 Std. überschreiten."},
		{"a kind of record among the values", 404,
			`{"error":{"code":"notFound","message":"not found","values":["project",7]}}`,
			"Projekt mit der Kennung 7 wurde nicht gefunden."},
		{"fields", 400, `{"error":{"message":"invalid","param":["durationHours","date"]}}`,
			"Ungültige Felder: Stunden, Datum"},
		{"a code the page has no sentence for", 409, `{"error":{"code":"somethingNew","message":"the installation's own words"}}`,
			"the installation's own words"},
		{"nothing to read", 502, `<html>`, "die Installation antwortete 502 Bad Gateway"},
		{"a token it does not know", 401, `{"error":{"message":"not authenticated"}}`,
			"die Installation kennt dieses Token nicht – leg unter Mein Konto, API-Tokens eines an und trag es in GTR_TOKEN ein"},
	} {
		if said := l.refusal(tc.status, []byte(tc.body)).Error(); said != tc.says {
			t.Errorf("%s was said as %q, want %q", tc.name, said, tc.says)
		}
	}

	// In English the installation's sentence is said as it wrote it.
	english := language{}.refusal(409, []byte(`{"error":{"code":"projectClosedForBooking","message":"closed","values":["Website","completed"]}}`))
	if english.Error() != "closed" {
		t.Errorf("in English the refusal was said as %q, want the installation's own words", english)
	}
}

// Every way REFUSAL_VALUES writes a value is one track knows how to write: a new
// one would otherwise reach a sentence as the word it travels in.
func TestEveryWayAValueIsWrittenIsOneTrackKnows(t *testing.T) {
	named := web.RefusalValues()
	if len(named) == 0 {
		t.Fatal("REFUSAL_VALUES read as nothing; the reading has stopped finding it")
	}

	writers := speaking("de").valueWriters()

	for code, values := range named {
		for at, kind := range values {
			if writers[kind] == nil {
				t.Errorf("REFUSAL_VALUES writes value %d of %s with %s, which track does not know", at, code, kind)
			}
		}
	}
}

// Hours, figures and days are written the way the web interface writes them in
// each language.
func TestHoursFiguresAndDaysAreWrittenTheWayThePageWritesThem(t *testing.T) {
	de, en := speaking("de"), language{}
	tuesday := time.Date(2026, time.October, 27, 12, 0, 0, 0, time.UTC)

	for _, tc := range []struct{ got, want string }{
		{en.hours(2.5), "2.50 h"},
		{de.hours(2.5), "2,50 Std."},
		{de.hours(1234.5), "1.234,50 Std."},
		{en.number(1234567.891), "1,234,567.89"},
		{de.number(-0.004), "0,00"},
		{de.number(-1.5), "-1,50"},
		{en.date("2026-10-21"), "2026-10-21"},
		{de.date("2026-10-21"), "21.10.2026"},
		{de.date("someday"), "someday"},
		{en.weekday(tuesday), "Tuesday, 27 October"},
		{de.weekday(tuesday), "Dienstag, 27. Oktober"},
		{strings.Join(de.refusalValues("x", []any{1.0, 1.25, "x"}), " "), "1 1,25 x"},
	} {
		if tc.got != tc.want {
			t.Errorf("wrote %q, want %q", tc.got, tc.want)
		}
	}
}

// The screen speaks German too: the stopwatch's card, today's list, the legend,
// the forms and the keys huh names under them.
func TestTheScreenSpeaksGerman(t *testing.T) {
	f, server := newFakeInstallation(t)

	de := speaking("de")
	m := newScreen(context.Background(), &session{api: newClient(server.URL, f.token, de), out: io.Discard, lang: de})

	read, _ := m.load()().(loaded)
	if read.err != nil {
		t.Fatalf("reading the day: %v", read.err)
	}

	m = m.read(read)
	m.width = 100

	shows := ansi.Strip(m.View().Content)
	for _, said := range []string{"seit 09:12", "Heute", "3,00 Std.", "2,50 Std.  Consulting", "kein Projekt",
		"w wechseln", "x stoppen", "b buchen", "r neu lesen", "q beenden"} {
		if !strings.Contains(shows, said) {
			t.Errorf("the screen shows\n%s\nwithout %q", shows, said)
		}
	}

	opened, _ := m.open("switch")
	shows = ansi.Strip(opened.(screen).View().Content)

	for _, said := range []string{"Auf ein anderes Projekt wechseln", "wird auf Consulting gebucht", "Kein Projekt",
		"Beschreibung", "esc abbrechen", "auswählen"} {
		if !strings.Contains(shows, said) {
			t.Errorf("the form shows\n%s\nwithout %q", shows, said)
		}
	}

	ended := m
	ended.now = ended.readAt

	if farewell := ended.farewell(ended.readAt); !strings.HasPrefix(farewell, "Läuft seit 09:12 auf Consulting") {
		t.Errorf("the screen leaves %q behind, want the German line", farewell)
	}
}

// looked is one key this package looks up, with its English and where.
type looked struct {
	english, at string
}

// lookups is every key this package passes to text, with its English: a key
// must be written out, or nothing could hold it to a dictionary.
func lookups(t *testing.T) map[string]looked {
	t.Helper()

	found := map[string]looked{}

	for _, file := range packageFiles(t) {
		ast.Inspect(file.syntax, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) < 2 {
				return true
			}

			if selector, ok := call.Fun.(*ast.SelectorExpr); !ok || selector.Sel.Name != "text" {
				return true
			}

			key, ok := call.Args[0].(*ast.BasicLit)
			if !ok || key.Kind != token.STRING {
				t.Errorf("%s looks up a key that is not written out", file.fset.Position(call.Pos()))

				return true
			}

			name, _ := strconv.Unquote(key.Value)
			english := ""

			if literal, ok := call.Args[1].(*ast.BasicLit); ok && literal.Kind == token.STRING {
				english, _ = strconv.Unquote(literal.Value)
			}

			found[name] = looked{english: english, at: file.fset.Position(key.Pos()).String()}

			return true
		})
	}

	if len(found) < 50 {
		t.Fatalf("found %d keys looked up; the reading has stopped finding them", len(found))
	}

	return found
}

// Every key track looks up is in every dictionary, every track.* entry is one it
// still looks up, and each fills in the places its English does.
func TestEveryKeyTrackLooksUpIsInEveryDictionary(t *testing.T) {
	used := lookups(t)

	for _, language := range i18n.Languages()[1:] {
		words := i18n.Dictionary(language)

		for _, key := range sortedKeys(used) {
			sentence, ok := words[key]
			if !ok {
				t.Errorf("%s looks up %s, which %s has no words for", used[key].at, key, language)

				continue
			}

			if english := used[key].english; english != "" && !slices.Equal(places(english), places(sentence)) {
				t.Errorf("%s fills in %v in English and %v in %s", key, places(english), places(sentence), language)
			}
		}

		for _, key := range sortedKeys(words) {
			if _, ok := used[key]; strings.HasPrefix(key, "track.") && !ok {
				t.Errorf("%s has %s, which track no longer looks up", language, key)
			}
		}
	}
}

// place is one of {0}, {1} ... in a sentence.
var place = regexp.MustCompile(`\{\d+\}`)

// places is which values a sentence takes, each once, in order.
func places(sentence string) []string {
	found := place.FindAllString(sentence, -1)

	sort.Strings(found)

	return slices.Compact(found)
}

// Nothing this package says to somebody is written as a bare string, where a
// dictionary cannot reach it: every literal that reads as words is a key or
// the English handed to text, or the usage.
func TestNoSentenceIsSaidAroundTheDictionaries(t *testing.T) {
	prose := regexp.MustCompile(`[A-Za-z]{2,} [A-Za-z]{2,}`)

	for _, file := range packageFiles(t) {
		inside := map[ast.Node]bool{}

		ast.Inspect(file.syntax, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.CallExpr:
				if selector, ok := node.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "text" {
					for _, arg := range node.Args[:min(2, len(node.Args))] {
						inside[arg] = true
					}
				}
			case *ast.ValueSpec:
				if slices.ContainsFunc(node.Names, func(name *ast.Ident) bool { return name.Name == "usage" }) {
					ast.Inspect(node, func(m ast.Node) bool { inside[m] = true; return true })
				}
			case *ast.ImportSpec:
				inside[node.Path] = true
			case *ast.Field:
				if node.Tag != nil {
					inside[node.Tag] = true
				}
			}

			literal, ok := n.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING || inside[literal] {
				return true
			}

			text, _ := strconv.Unquote(literal.Value)
			if !prose.MatchString(text) {
				return true
			}

			t.Errorf("%s says %q around the dictionaries; look it up with text", file.fset.Position(literal.Pos()), text)

			return true
		})
	}
}

// parsedFile is one file of this package, as the toolchain reads it.
type parsedFile struct {
	fset   *token.FileSet
	syntax *ast.File
}

// packageFiles are this package's files, tests aside.
func packageFiles(t *testing.T) []parsedFile {
	t.Helper()

	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}

	var files []parsedFile

	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}

		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}

		fset := token.NewFileSet()

		syntax, err := parser.ParseFile(fset, name, source, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}

		files = append(files, parsedFile{fset: fset, syntax: syntax})
	}

	return files
}

// sortedKeys is a map's keys in order.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}
