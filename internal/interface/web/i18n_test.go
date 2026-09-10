package web_test

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The interface is written in English and every other language is a dictionary
// layered over it. That is what makes English the fallback: a key nobody has
// translated yet still renders, in English, instead of falling through to a
// language the reader may not know.
//
// These tests read the shipped assets rather than a copy, so they fail if the
// arrangement is ever undone by hand.

func asset(t *testing.T, path string) string {
	t.Helper()

	rec := get(t, http.MethodGet, path)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for %s, got %d", path, rec.Code)
	}

	return rec.Body.String()
}

var (
	// A dictionary: `  <lang>: {` up to the matching `  },`. The empty form
	// `{}` is matched first, or the search would run past it and swallow the
	// next language's entries.
	dictPattern = regexp.MustCompile(`(?s)\n  ([a-z]{2}): \{(\}|.*?\n  \}),`)

	// One entry, whose value may be several literals joined by "+".
	entryPattern = regexp.MustCompile(`(?s)'([^']+)':\s*((?:'(?:[^'\\]|\\.)*'\s*\+?\s*)+),`)
)

// dictionaries reads the translation tables out of app.js.
func dictionaries(t *testing.T) map[string]map[string]string {
	t.Helper()

	js := asset(t, "/app.js")

	start := strings.Index(js, "const TRANSLATIONS = {")
	if start < 0 {
		t.Fatal("app.js no longer declares TRANSLATIONS")
	}

	out := map[string]map[string]string{}

	for _, dict := range dictPattern.FindAllStringSubmatch(js[start:], -1) {
		entries := map[string]string{}
		for _, entry := range entryPattern.FindAllStringSubmatch(dict[2], -1) {
			entries[entry[1]] = entry[2]
		}

		out[dict[1]] = entries
	}

	if len(out) == 0 {
		t.Fatal("no dictionaries found; the TRANSLATIONS layout changed")
	}

	return out
}

// codeKeys collects every translation key the script looks up through t().
//
// The markup is only half the interface. Everything rendered from JavaScript -
// every toast, the guided tour, the setup wizard, the status lines - names its
// key in a t() call instead, and a key missing there fails in exactly the way
// this file exists to prevent: silently, in English, until somebody switches
// language and finds one sentence in the wrong one.
//
// The leading boundary matters. Without it the pattern also matches the tail of
// any identifier ending in t - set('from', …), Format('en-CA', …) - and reports
// argument values as untranslated keys.
var codeKeyPattern = regexp.MustCompile(`(?:^|[^A-Za-z0-9_$.])t\('([^']+)'\s*,`)

func codeKeys(t *testing.T) []string {
	t.Helper()

	js := asset(t, "/app.js")

	// Without the dictionaries, or every German entry would be read back as a
	// key the code looks up.
	start := strings.Index(js, "const TRANSLATIONS = {")
	end := strings.Index(js[start:], "\n};")

	if start < 0 || end < 0 {
		t.Fatal("could not locate the TRANSLATIONS block")
	}

	code := js[:start] + js[start+end:]

	seen := map[string]bool{}
	for _, m := range codeKeyPattern.FindAllStringSubmatch(code, -1) {
		seen[m[1]] = true
	}

	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}

// markupKeys collects every translation key the page markup refers to.
func markupKeys(t *testing.T) []string {
	t.Helper()

	html := asset(t, "/")

	seen := map[string]bool{}

	for _, attribute := range []string{"data-i18n", "data-i18n-placeholder", "data-i18n-aria"} {
		pattern := regexp.MustCompile(attribute + `="([^"]+)"`)
		for _, m := range pattern.FindAllStringSubmatch(html, -1) {
			seen[m[1]] = true
		}
	}

	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}

// English lives in the markup, so an "en" dictionary would be a second source
// of truth that could drift from what the page actually says.
func TestEnglishIsTheSourceNotATranslation(t *testing.T) {
	dicts := dictionaries(t)

	english, ok := dicts["en"]
	if !ok {
		t.Fatal("expected an en entry in TRANSLATIONS, even if empty")
	}

	if len(english) != 0 {
		t.Errorf("en must stay empty because the markup already carries English, "+
			"but it holds %d entries", len(english))
	}
}

// A key the markup uses but a translation lacks shows English there. That is
// the intended fallback, not a licence to leave gaps.
func TestEveryTranslationCoversTheMarkup(t *testing.T) {
	dicts := dictionaries(t)
	keys := markupKeys(t)

	if len(keys) == 0 {
		t.Fatal("no translation keys found in the markup")
	}

	for language, dict := range dicts {
		if language == "en" {
			continue
		}

		var missing []string

		for _, key := range keys {
			if _, ok := dict[key]; !ok {
				missing = append(missing, key)
			}
		}

		if len(missing) > 0 {
			t.Errorf("%s is missing %d key(s), which would render in English: %v",
				language, len(missing), missing)
		}
	}
}

// The same for what the script renders. Without this the guided tour and the
// setup wizard - which are almost entirely t() calls - could lose their
// translation one key at a time with every test still passing.
func TestEveryTranslationCoversTheCodeLookups(t *testing.T) {
	dicts := dictionaries(t)
	keys := codeKeys(t)

	if len(keys) == 0 {
		t.Fatal("no t() lookups found in app.js; the pattern no longer matches how keys are looked up")
	}

	// The tour and the wizard are the two the eye skips over in review, so their
	// absence is called out rather than left to be noticed in a list of forty.
	for _, prefix := range []string{"tour.", "setup."} {
		if !anyWithPrefix(keys, prefix) {
			t.Errorf("no %s keys found at all, which means they stopped going through t() "+
				"and nothing here checks them any more", prefix)
		}
	}

	for language, dict := range dicts {
		if language == "en" {
			continue
		}

		var missing []string

		for _, key := range keys {
			if _, ok := dict[key]; !ok {
				missing = append(missing, key)
			}
		}

		if len(missing) > 0 {
			t.Errorf("%s is missing %d key(s) the script looks up, which would render in English: %v",
				language, len(missing), missing)
		}
	}
}

func anyWithPrefix(keys []string, prefix string) bool {
	for _, key := range keys {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}

	return false
}

// A dictionary entry nobody looks up is dead weight that outlives the text it
// once translated.
func TestNoTranslationIsUnused(t *testing.T) {
	dicts := dictionaries(t)
	js := asset(t, "/app.js")
	html := asset(t, "/")

	for language, dict := range dicts {
		if language == "en" {
			continue
		}

		var unused []string

		for key := range dict {
			// Either the markup names it, or code looks it up via t(), or code hands
			// it to swapTheLabel - which is a lookup with a second job: it declares
			// the key on the element as well, so the next language change translates
			// the message the screen is actually showing rather than the one the
			// markup was written with.
			if strings.Contains(html, `"`+key+`"`) || strings.Contains(js, `t('`+key+`',`) {
				continue
			}

			if swappedLabelKeys(t)[key] {
				continue
			}

			// The err.* sentences are looked up by the code the server sent, and the
			// field.* names by the field it rejected, so no literal mentions either.
			// Both have stricter guards of their own -
			// TestEveryServerErrorCodeIsTranslated and TestEveryRejectedFieldIsNamed -
			// which check them against the Go source in both directions.
			if strings.HasPrefix(key, "err.") {
				continue
			}

			if field, isField := strings.CutPrefix(key, "field."); isField {
				if _, rejected := rejectedFields(t)[field]; rejected {
					continue
				}
			}

			// Same again for the restart refusals, which are looked up by the code
			// the server sent. TestEveryRestartRefusalIsExplained checks these
			// against the restart package in both directions.
			if code, isRefusal := strings.CutPrefix(key, "restart.unsupported."); isRefusal {
				if _, known := restartRefusalCodes(t)[code]; known {
					continue
				}
			}

			// And the project states, looked up as t(`status.${status}`) from
			// whatever the server sent. TestEveryProjectStateIsNamed checks these
			// against the model.
			if state, isState := strings.CutPrefix(key, "status."); isState {
				if _, known := projectStates(t)[state]; known {
					continue
				}
			}

			// What a right is called and what it allows, looked up as
			// t(`perm.${right}`) and t(`perm.desc.${right}`) from whatever the server
			// listed, and the areas as t(`perm.group.${area}`) from the first part of
			// each identifier. TestEveryPermissionIsNamedAndExplained checks all three
			// against the model, in both directions.
			if rest, isPerm := strings.CutPrefix(key, "perm."); isPerm {
				right := strings.TrimPrefix(strings.TrimPrefix(rest, "desc."), "group.")

				if _, known := permissionRights(t)[right]; known {
					continue
				}

				if _, known := permissionAreas(t)[right]; known {
					continue
				}
			}

			// What a role is called and what it is for, looked up as
			// t(`role.name.${name}`) and t(`role.desc.${name}`) from whatever the server
			// sent. TestEverySeededRoleSaysWhatItIsFor checks both against the roles the
			// application ships, in both directions.
			if _, isRole := strings.CutPrefix(key, "role.desc."); isRole {
				continue
			}

			if _, isRole := strings.CutPrefix(key, "role.name."); isRole {
				continue
			}

			// Why one row of an imported file was refused, looked up as
			// t(`row.${code}`) from the code the server sent with it.
			// TestEveryImportRowProblemIsTranslated checks these against the Go source
			// in both directions.
			if code, isRow := strings.CutPrefix(key, "row."); isRow {
				if _, known := importRowProblemCodes(t)[code]; known {
					continue
				}
			}

			// The per-table spreadsheet cards, looked up as t(`sheet.${key}.text`)
			// from the table being built. TestEverySheetCardIsNamed checks these
			// against the card list in both directions.
			if rest, isCard := strings.CutPrefix(key, "sheet."); isCard {
				if table, _, cut := strings.Cut(rest, "."); cut {
					if _, known := sheetCards(t)[table]; known {
						continue
					}
				}
			}

			unused = append(unused, key)
		}

		sort.Strings(unused)

		if len(unused) > 0 {
			t.Errorf("%s has %d entry(s) nothing refers to: %v", language, len(unused), unused)
		}
	}
}

// The fallbacks handed to t() are what shows when a key is absent, so they must
// be the English text rather than another language's.
// swappedLabelKeys returns the keys handed to swapTheLabel, which declares a key
// on an element as well as translating it. Matched on the call rather than on
// "some quoted word in the middle of a call", so a key that only appears as an
// unrelated argument somewhere is still reported as unused.
func swappedLabelKeys(t *testing.T) map[string]bool {
	t.Helper()

	js := asset(t, "/app.js")
	keys := map[string]bool{}

	for _, match := range regexp.MustCompile(`swapTheLabel\([^,]+,\s*'([^']+)'`).
		FindAllStringSubmatch(js, -1) {
		keys[match[1]] = true
	}

	if len(keys) == 0 {
		t.Fatal("no swapTheLabel call found in app.js; this helper is reading nothing")
	}

	return keys
}

func TestCodeFallbacksAreEnglish(t *testing.T) {
	js := asset(t, "/app.js")

	start := strings.Index(js, "const TRANSLATIONS = {")
	end := strings.Index(js[start:], "\n};")

	if start < 0 || end < 0 {
		t.Fatal("could not locate the TRANSLATIONS block")
	}

	// Everything except the dictionaries themselves, which are allowed to hold
	// other languages.
	code := js[:start] + js[start+end:]

	// Accented Latin, Greek or Cyrillic letters mean another language leaked
	// into the source. Typographic punctuation is not included: an ellipsis or
	// an em dash is ordinary in English too.
	suspicious := regexp.MustCompile(
		`t\('[^']+',\s*'[^']*[\x{00C0}-\x{024F}\x{0370}-\x{04FF}][^']*'\)`)
	if found := suspicious.FindAllString(code, -1); found != nil {
		t.Errorf("these t() fallbacks do not look like English: %v", found)
	}

	// An accent is not required to write German, so the check above misses the
	// realistic slip: a fallback typed in German that happens to have no umlaut
	// in it. These words are common enough that one of them appearing in what is
	// supposed to be English is worth stopping for, and rare enough as English
	// that a false positive is easy to reword around.
	german := regexp.MustCompile(`(?i)\b(und|oder|nicht|wurde|werden|wird|kann|` +
		`keine|keinen|eine|einen|einer|nur|noch|schon|bitte|wieder|` +
		`gespeichert|fehlgeschlagen|einstellungen|angemeldet|geloescht)\b`)

	for _, call := range regexp.MustCompile(`t\('[^']+',\s*'([^'\\]|\\.)*'`).FindAllString(code, -1) {
		if word := german.FindString(call); word != "" {
			t.Errorf("this t() fallback has the German word %q in it, "+
				"but a fallback is what shows when a translation is missing "+
				"and therefore has to be English: %s", word, call)
		}
	}
}

// A translation is set with textContent, so an HTML entity in one renders as the
// entity.
//
// "API-Dokumentation &#8599;" appeared on screen exactly like that. The markup may
// carry entities - it is parsed as HTML - but a dictionary value never is.
func TestNoTranslationCarriesAnHTMLEntity(t *testing.T) {
	entity := regexp.MustCompile(`&(#[0-9]+|#x[0-9a-fA-F]+|[a-zA-Z]+);`)

	for language, dict := range dictionaries(t) {
		for key, value := range dict {
			if found := entity.FindString(value); found != "" {
				t.Errorf("%s[%q] contains the HTML entity %s, which renders literally; "+
					"write the character itself", language, key, found)
			}
		}
	}
}

// readSource reads a file from the repository for a test to assert against.
func readSource(t *testing.T, path string) string {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	return string(raw)
}

// withoutLineComments drops // comments so a name discussed in prose is not read
// as a name the code uses.
func withoutLineComments(js string) string {
	var out strings.Builder

	for line := range strings.SplitSeq(js, "\n") {
		if at := strings.Index(line, "//"); at >= 0 {
			line = line[:at]
		}

		out.WriteString(line)
		out.WriteString("\n")
	}

	return out.String()
}

// Words come from the language; conventions come from the locale.
//
// Two questions that look like one. Which dictionary to read is answered by a
// language this application ships words for - de or en, and nothing else would
// resolve. How to write a date is answered by the reader's own locale, which
// carries a region: there is one English dictionary and there is not one English
// date, so formatting an en-GB browser as plain "en" writes the twelfth of
// August as 08/12/2026.
//
// The two resolvers differ by one word at the call site, which is exactly how
// they get mixed up again. This pins each Intl constructor to the locale and the
// dictionary lookup to the language.
func TestFormattingFollowsTheLocaleAndWordsFollowTheLanguage(t *testing.T) {
	js := withoutLineComments(asset(t, "/app.js"))

	// Every Intl constructor and every toLocale* call decides how something is
	// written, so every one of them takes the locale.
	formatters := regexp.MustCompile(`(?:new Intl\.\w+|\.toLocale\w+)\(\s*activeLanguage\(\)`)
	if found := formatters.FindAllString(js, -1); len(found) > 0 {
		t.Errorf("%d formatter(s) are given the language rather than the locale, so a "+
			"reader whose browser has a region loses it: %v", len(found), found)
	}

	// And the dictionary is not looked up by a locale, which would miss: the
	// table is keyed on "de", and "de-AT" is not a key in it.
	if strings.Contains(js, "TRANSLATIONS[activeLocale()]") {
		t.Error("the dictionary is looked up by locale, so any browser with a region " +
			"falls through to the English fallback for every key")
	}

	// Both have to still exist and be used, or this passes by them being gone.
	for _, needed := range []string{"activeLocale()", "activeLanguage()"} {
		if !strings.Contains(js, needed) {
			t.Errorf("%s is not called anywhere; this test is no longer checking a "+
				"distinction the code makes", needed)
		}
	}

	if !regexp.MustCompile(`new Intl\.\w+\(\s*activeLocale\(\)`).MatchString(js) {
		t.Error("no formatter is given the locale, so nothing is actually formatted " +
			"the reader's way")
	}
}

// One word for an hour, everywhere it is written.
//
// The unit is looked up as unit.hours, and it was also spelled out inside half a
// dozen sentences - so German read "5,01 h gesamt" beside a table writing
// "5,01 Std.", because the sentence and the dictionary were two places to
// remember the same word and only one of them was updated.
//
// The sentences that could drop it did: their call sites pass a figure that
// already carries its unit. The two that cannot are the refusals whose numbers
// come from the server, where one of the interpolated values is a date rather
// than an amount - those keep the word, and this is what keeps it the same word.
func TestOneWordForAnHour(t *testing.T) {
	dict, ok := dictionaries(t)["de"]
	if !ok {
		t.Fatal("app.js has no German dictionary")
	}

	unit, ok := dict["unit.hours"]
	if !ok {
		t.Fatal("unit.hours is gone; the hour unit is no longer translated at all")
	}

	// The stored form is a JavaScript literal, quotes and all.
	unit = strings.Trim(unit, "'")

	// A bare "h" used as a unit: after a placeholder, or after a figure, and
	// followed by the end, a bracket or a space. Deliberately narrow - "h" occurs
	// inside plenty of German words, and this is only looking for the unit.
	bare := regexp.MustCompile(`\{\d\}\s+h\b|\d\s+h\b`)

	for key, value := range dict {
		if key == "unit.hours" || !bare.MatchString(value) {
			continue
		}

		if strings.Contains(value, unit) {
			continue
		}

		t.Errorf("%s writes the hour unit as a bare \"h\" while unit.hours is %q, so "+
			"one screen says one and one says the other: %s", key, unit, value)
	}
}

// The installer's own refusals have their own translations.
//
// It cannot use the dictionary in app.js: it runs before there is a database, so
// nothing of the application is loaded, and it is served as one self-contained
// page on purpose. So it carries a small table of its own - and a code it sends
// without an entry there is shown as the English the server wrote, on a screen
// that is otherwise translated.
func TestTheInstallerTranslatesItsOwnRefusals(t *testing.T) {
	root := filepath.Join("..", "installer")

	source, err := os.ReadFile(filepath.Join(root, "installer.go"))
	if err != nil {
		t.Fatalf("reading the installer: %v", err)
	}

	page, err := os.ReadFile(filepath.Join(root, "assets", "install.html"))
	if err != nil {
		t.Fatalf("reading the installer page: %v", err)
	}

	codes := regexp.MustCompile(`WithCode\("([^"]+)"`).FindAllSubmatch(source, -1)
	if len(codes) == 0 {
		t.Fatal("the installer sends no coded refusals; this test is reading nothing")
	}

	for _, match := range codes {
		key := "'err." + string(match[1]) + "'"

		if !strings.Contains(string(page), key) {
			t.Errorf("the installer sends %s and its page has no %s, so a German "+
				"reader is shown the English sentence", match[1], key)
		}
	}

	// The fields it refuses, which is the other half and the half that was
	// reported: "invalid field(s): name" named the payload key rather than the
	// label above the box.
	for _, field := range []string{"name", "host", "port", "user"} {
		if !strings.Contains(string(page), "'field."+field+"'") {
			t.Errorf("the installer can refuse %q and its page cannot name that field",
				field)
		}
	}
}
