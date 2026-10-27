package cli

import (
	"context"
	"fmt"
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
)

// The language is what was asked for on the line, then in GTR_LANG, then what
// the environment and the system are set to, then English - and a language that
// was asked for and is not had is refused, in the language track falls back to.
func TestTheLanguageIsChosenInTheOrderItWasAskedFor(t *testing.T) {
	for _, tc := range []struct {
		name    string
		flag    string
		env     map[string]string
		system  string
		german  bool
		refused string
	}{
		{name: "nothing set anywhere", german: false},
		{name: "the flag", flag: "de", german: true},
		{name: "the flag over GTR_LANG", flag: "en", env: map[string]string{"GTR_LANG": "de"}, german: false},
		{name: "GTR_LANG", env: map[string]string{"GTR_LANG": "de"}, german: true},
		{name: "GTR_LANG as a locale", env: map[string]string{"GTR_LANG": "de_DE.UTF-8"}, german: true},
		{name: "GTR_LANG over LANG", env: map[string]string{"GTR_LANG": "en-GB", "LANG": "de_DE.UTF-8"}, german: false},
		{name: "LANG", env: map[string]string{"LANG": "de_DE.UTF-8"}, german: true},
		{name: "LC_ALL over LANG", env: map[string]string{"LC_ALL": "en_US.UTF-8", "LANG": "de_DE.UTF-8"}, german: false},
		{name: "LC_MESSAGES over LANG", env: map[string]string{"LC_MESSAGES": "de_AT", "LANG": "en_US"}, german: true},
		{name: "the environment over the system", env: map[string]string{"LANG": "C.UTF-8"}, system: "de-DE", german: false},
		{name: "the system", system: "de-DE", german: true},
		{name: "a system in a language track does not have", system: "fr-FR", german: false},
		{name: "a LANG in a language track does not have", env: map[string]string{"LANG": "fr_FR.UTF-8"}, german: false},
		{name: "a LANG that only starts with the letters", env: map[string]string{"LANG": "default"}, german: false},
		{name: "a flag naming one it does not have", flag: "fr", env: map[string]string{"LANG": "de_DE"},
			german: true, refused: `--lang nimmt de oder en, nicht "fr"`},
		{name: "a flag naming one it does not have, under a GTR_LANG it has", flag: "fr",
			env: map[string]string{"GTR_LANG": "de"}, german: true, refused: `--lang nimmt de oder en, nicht "fr"`},
		{name: "GTR_LANG naming one it does not have", env: map[string]string{"GTR_LANG": "english"},
			german: false, refused: `GTR_LANG takes de or en, not "english"`},
		{name: "a stray GTR_LANG under a flag it has", flag: "de", env: map[string]string{"GTR_LANG": "english"},
			german: true},
	} {
		env := func(key string) string { return tc.env[key] }
		asked := 0

		chosen, err := chooseLanguage(tc.flag, env, func() string { asked++; return tc.system })

		if tc.env["LANG"] != "" && asked > 0 {
			t.Errorf("%s: the system was asked although LANG says %q", tc.name, tc.env["LANG"])
		}

		if chosen.german != tc.german {
			t.Errorf("%s: German is %v, want %v", tc.name, chosen.german, tc.german)
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
		{[]string{"--lang", "de", "status"}, []string{"Läuft seit 09:12 auf Consulting – bisher 1:30.", "Heute gebucht: 3,00 Std."}},
		{[]string{"book", "1:30", "consult", "-d", "2026-10-21", "--lang=de"}, []string{"1,50 Std. auf Consulting für den 21.10.2026 gebucht."}},
		{[]string{"today", "--lang", "de"}, []string{"   2,50 Std.  Consulting - review", "   0,50 Std.  kein Projekt", "   3,00 Std.  insgesamt"}},
		{[]string{"help", "--lang", "de"}, []string{"Zeit im Terminal buchen", "--lang de|en"}},
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
	}

	// And what cannot be read, in German too - from GTR_LANG this time.
	env := map[string]string{"GTR_URL": server.URL, "GTR_TOKEN": f.token, "GTR_LANG": "de"}
	if status, _, errOut := trackWith(t, env, "book", "viel"); status != 2 || !strings.Contains(errOut, `"viel" ist keine Stundenzahl`) {
		t.Errorf("unreadable hours answered %d %q, want 2 in German", status, errOut)
	}

	env["GTR_TOKEN"] = "gtr_wrong"
	if status, _, errOut := trackWith(t, env, "status"); status != 1 || !strings.Contains(errOut, "Mein Konto, API-Tokens") {
		t.Errorf("an unknown token answered %d %q, want 1 in German", status, errOut)
	}
}

// A refusal is said in the browser's German: the sentence the web interface has
// for its code, with a status, a kind of record and a day written as the page
// writes them, the fields named as its forms name them - and the installation's
// own English where the page has no sentence for it.
func TestARefusalIsSaidInTheBrowsersGerman(t *testing.T) {
	l := inGerman()

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
// one would otherwise reach a German sentence as the word it travels in.
func TestEveryWayAValueIsWrittenIsOneTrackKnows(t *testing.T) {
	named := web.RefusalValues()
	if len(named) == 0 {
		t.Fatal("REFUSAL_VALUES read as nothing; the reading has stopped finding it")
	}

	writers := inGerman().valueWriters()

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
	de, en := inGerman(), language{}

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
		{en.weekday(time.Date(2026, time.October, 27, 12, 0, 0, 0, time.UTC)), "Tuesday, 27 October"},
		{de.weekday(time.Date(2026, time.October, 27, 12, 0, 0, 0, time.UTC)), "Dienstag, 27. Oktober"},
		{de.fillIn("{0} und {1}, {2} und {9}", []any{1.0, 1.25, "x"}), "1 und 1,25, x und {9}"},
		{de.fillIn("{ offen {0", []any{"x"}), "{ offen {0"},
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

	de := inGerman()
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

// translated is every sentence this package passes through tr, with where.
func translated(t *testing.T) map[string]string {
	t.Helper()

	said := map[string]string{}

	for _, file := range packageFiles(t) {
		ast.Inspect(file.syntax, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}

			if selector, ok := call.Fun.(*ast.SelectorExpr); !ok || selector.Sel.Name != "tr" {
				return true
			}

			literal, ok := call.Args[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				t.Errorf("%s passes tr something other than a sentence, which nothing can hold to a translation",
					file.fset.Position(call.Pos()))

				return true
			}

			sentence, _ := strconv.Unquote(literal.Value)
			said[sentence] = file.fset.Position(literal.Pos()).String()

			return true
		})
	}

	if len(said) < 50 {
		t.Fatalf("found %d sentences passed through tr; the reading has stopped finding them", len(said))
	}

	return said
}

// Every sentence track says has its German, every German entry is a sentence
// track still says, and each says what its English says with the same values.
func TestEverySentenceHasItsGerman(t *testing.T) {
	said := translated(t)

	for _, sentence := range sortedKeys(said) {
		if _, ok := german[sentence]; !ok {
			t.Errorf("%s says %q, which has no German", said[sentence], sentence)
		}
	}

	for _, sentence := range sortedKeys(german) {
		if _, ok := said[sentence]; !ok {
			t.Errorf("german translates %q, which nothing says any more", sentence)
		}
	}

	for sentence, translation := range german {
		if want, got := verbs(sentence), verbs(translation); !slices.Equal(want, got) {
			t.Errorf("%q fills in %v, but its German %q fills in %v", sentence, want, translation, got)
		}
	}
}

// verb is one of fmt's verbs, with the argument it takes and an optional index.
var verb = regexp.MustCompile(`%(?:\[(\d+)\])?[-+# 0-9.*]*([a-zA-Z])`)

// verbs is which argument each verb of a format takes and as what, in argument
// order: a translation may move them about with %[2]s, and must take the same.
func verbs(format string) []string {
	var taken []string

	next := 1

	for _, m := range verb.FindAllStringSubmatch(strings.ReplaceAll(format, "%%", ""), -1) {
		at := next
		if m[1] != "" {
			at, _ = strconv.Atoi(m[1])
		}

		taken = append(taken, fmt.Sprintf("%d:%s", at, m[2]))
		next = at + 1
	}

	sort.Strings(taken)

	return taken
}

// Nothing this package says to somebody is written as a bare string, where tr
// cannot reach it: every literal that reads as words is a sentence passed to tr,
// a German one, or the usage.
func TestNoSentenceIsSaidAroundTheDictionary(t *testing.T) {
	prose := regexp.MustCompile(`[A-Za-z]{2,} [A-Za-z]{2,}`)

	for _, file := range packageFiles(t) {
		inside := map[ast.Node]bool{}

		ast.Inspect(file.syntax, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.CallExpr:
				if selector, ok := node.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "tr" {
					for _, arg := range node.Args {
						inside[arg] = true
					}
				}
			case *ast.ValueSpec:
				for _, name := range node.Names {
					if name.Name == "german" || name.Name == "usage" || name.Name == "usageGerman" ||
						name.Name == "germanWeekdays" || name.Name == "germanMonths" {
						ast.Inspect(node, func(m ast.Node) bool { inside[m] = true; return true })
					}
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

			t.Errorf("%s says %q around the dictionary; pass it through tr", file.fset.Position(literal.Pos()), text)

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
