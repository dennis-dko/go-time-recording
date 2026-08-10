package web_test

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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
			// Either the markup names it, or code looks it up via t().
			if strings.Contains(html, `"`+key+`"`) || strings.Contains(js, `t('`+key+`',`) {
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

// Every reason the server names has a sentence, and every sentence names a reason
// the server still gives.
//
// The interface looks these up as t(`err.${code}`), so no literal key appears in
// the source and neither the coverage test nor the unused test can see them. That
// leaves exactly the two ways for this to rot: a rule annotated in Go that nobody
// translated, which shows the reader English, and a sentence for a code that has
// since been renamed or deleted, which shows nothing. So this reads the codes out
// of the Go source and compares the two sets.
func TestEveryServerErrorCodeIsTranslated(t *testing.T) {
	codes := serverErrorCodes(t)
	if len(codes) == 0 {
		t.Fatal("no WithCode(...) calls found; this test is no longer reading the source")
	}

	dict, ok := dictionaries(t)["de"]
	if !ok {
		t.Fatal("app.js has no German dictionary")
	}

	var untranslated, orphaned []string

	for code := range codes {
		if _, found := dict["err."+code]; !found {
			untranslated = append(untranslated, code)
		}
	}

	for key := range dict {
		code, isError := strings.CutPrefix(key, "err.")
		if !isError {
			continue
		}

		if _, found := codes[code]; !found {
			orphaned = append(orphaned, key)
		}
	}

	sort.Strings(untranslated)
	sort.Strings(orphaned)

	if len(untranslated) > 0 {
		t.Errorf("%d error code(s) the server sends have no German sentence, "+
			"so the reader is shown English: %v", len(untranslated), untranslated)
	}

	if len(orphaned) > 0 {
		t.Errorf("%d German sentence(s) are for codes the server no longer sends: %v",
			len(orphaned), orphaned)
	}
}

// serverErrorCodes collects the codes attached with WithCode across the Go source.
func serverErrorCodes(t *testing.T) map[string]struct{} {
	t.Helper()

	// From this package up to the module root, which is where internal/ lives.
	root := filepath.Join("..", "..", "..")
	codes := map[string]struct{}{}
	pattern := regexp.MustCompile(`WithCode\("([^"]+)"`)

	err := filepath.WalkDir(filepath.Join(root, "internal"),
		func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if entry.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}

			// The doc comment on WithCode itself shows an example, which is not a
			// call site and would otherwise register as one.
			if strings.HasSuffix(path, filepath.Join("apperror", "apperror.go")) {
				return nil
			}

			source, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}

			for _, match := range pattern.FindAllSubmatch(source, -1) {
				codes[string(match[1])] = struct{}{}
			}

			return nil
		})
	if err != nil {
		t.Fatalf("walking the source: %v", err)
	}

	return codes
}

// A field the server can reject is named the way the screen names it.
//
// Rejections travel as a list of field names, which are column names:
// "dailyTargetHours" is not what the label above the box says, in any language.
// The interface looks each one up as t(`field.${name}`), so nothing in the source
// mentions the key and the coverage tests cannot see it - which is how a field
// could be rejected by name and shown as an identifier for good.
func TestEveryRejectedFieldIsNamed(t *testing.T) {
	fields := rejectedFields(t)
	if len(fields) == 0 {
		t.Fatal("no rejected field names found; this test is no longer reading the source")
	}

	dict, ok := dictionaries(t)["de"]
	if !ok {
		t.Fatal("app.js has no German dictionary")
	}

	var unnamed []string

	for field := range fields {
		if _, found := dict["field."+field]; !found {
			unnamed = append(unnamed, field)
		}
	}

	sort.Strings(unnamed)

	if len(unnamed) > 0 {
		t.Errorf("%d field(s) the server rejects have no German name, so a refusal "+
			"shows the column name: %v", len(unnamed), unnamed)
	}
}

// rejectedFields collects the field names that reach apperror.InvalidFields.
//
// Read out of the source rather than listed here, so a field added to a validation
// cannot quietly arrive without a name. Both shapes are covered: the names passed
// straight to InvalidFields, and the ones collected in a slice first, which is what
// the validations with several fields do.
func rejectedFields(t *testing.T) map[string]struct{} {
	t.Helper()

	root := filepath.Join("..", "..", "..")
	fields := map[string]struct{}{}

	patterns := []*regexp.Regexp{
		regexp.MustCompile(`InvalidFields\(([^)]*)\)`),
		regexp.MustCompile(`append\((?:invalid|fields), ([^)]*)\)`),
	}
	literal := regexp.MustCompile(`"([a-zA-Z][a-zA-Z0-9]*)"`)

	err := filepath.WalkDir(filepath.Join(root, "internal"),
		func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if entry.IsDir() || !strings.HasSuffix(path, ".go") ||
				strings.HasSuffix(path, "_test.go") {
				return nil
			}

			source, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}

			for _, pattern := range patterns {
				for _, call := range pattern.FindAllSubmatch(source, -1) {
					for _, name := range literal.FindAllSubmatch(call[1], -1) {
						fields[string(name[1])] = struct{}{}
					}
				}
			}

			return nil
		})
	if err != nil {
		t.Fatalf("walking the source: %v", err)
	}

	return fields
}

// Every refusal the restart package can name has a sentence, and every sentence
// names a refusal it can still give.
//
// There is more than one reason restarting can be impossible - Windows has no
// execve, and on unix the running binary can fail to be located - and the interface
// looks the sentence up by the code the server sent. One sentence for all of them
// told a Linux reader they were on Windows, which is how this guard came to exist.
func TestEveryRestartRefusalIsExplained(t *testing.T) {
	codes := restartRefusalCodes(t)
	if len(codes) == 0 {
		t.Fatal("no restart refusal codes found; this test is no longer reading the source")
	}

	dict, ok := dictionaries(t)["de"]
	if !ok {
		t.Fatal("app.js has no German dictionary")
	}

	var unexplained, orphaned []string

	for code := range codes {
		if _, found := dict["restart.unsupported."+code]; !found {
			unexplained = append(unexplained, code)
		}
	}

	for key := range dict {
		code, isRefusal := strings.CutPrefix(key, "restart.unsupported.")
		if !isRefusal {
			continue
		}

		// "other" is the interface's own fallback for a code it does not know,
		// which by definition the server never sends.
		if code == "other" {
			continue
		}

		if _, known := codes[code]; !known {
			orphaned = append(orphaned, key)
		}
	}

	sort.Strings(unexplained)
	sort.Strings(orphaned)

	if len(unexplained) > 0 {
		t.Errorf("%d restart refusal(s) have no German sentence, so the reader is shown "+
			"English: %v", len(unexplained), unexplained)
	}

	if len(orphaned) > 0 {
		t.Errorf("%d sentence(s) are for refusals the server no longer gives: %v",
			len(orphaned), orphaned)
	}
}

// restartRefusalCodes reads what restart.Code() can return, out of the source.
//
// Both build-tagged files, because only one of them is compiled here and the other
// is the one that matters for the platform this is about.
func restartRefusalCodes(t *testing.T) map[string]struct{} {
	t.Helper()

	dir := filepath.Join("..", "..", "..", "internal", "infrastructure", "restart")

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading the restart package: %v", err)
	}

	codes := map[string]struct{}{}

	// Up to the first closing brace, which is the end of the one-line body or of
	// the inner if - both of which have the literal in front of them. Anything
	// greedier runs into the next function's doc comment and reads prose as a code.
	body := regexp.MustCompile(`func Code\(\) string \{[^}]*`)

	// Only what is returned, so a comment inside the body cannot contribute one.
	literal := regexp.MustCompile(`return "([a-zA-Z][a-zA-Z0-9]*)"`)

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") ||
			strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}

		source, readErr := os.ReadFile(filepath.Join(dir, entry.Name()))
		if readErr != nil {
			t.Fatalf("reading %s: %v", entry.Name(), readErr)
		}

		for _, fn := range body.FindAll(source, -1) {
			for _, match := range literal.FindAllSubmatch(fn, -1) {
				codes[string(match[1])] = struct{}{}
			}
		}
	}

	return codes
}

// Every state a project can be in has a German word for it.
//
// The badge is rendered as t(`status.${status}`) from whatever the server sent, so
// no literal key appears in the source and the coverage tests cannot see these.
// Without a word they render as the raw value, which is how "active" sat in the
// middle of an otherwise German table.
func TestEveryProjectStateIsNamed(t *testing.T) {
	states := projectStates(t)
	if len(states) == 0 {
		t.Fatal("no project states found; this test is no longer reading the source")
	}

	dict, ok := dictionaries(t)["de"]
	if !ok {
		t.Fatal("app.js has no German dictionary")
	}

	var unnamed []string

	for state := range states {
		if _, found := dict["status."+state]; !found {
			unnamed = append(unnamed, state)
		}
	}

	sort.Strings(unnamed)

	if len(unnamed) > 0 {
		t.Errorf("%d project state(s) have no German word, so the badge shows the raw "+
			"value: %v", len(unnamed), unnamed)
	}
}

// projectStates reads what a project's status can be, out of the model.
func projectStates(t *testing.T) map[string]struct{} {
	t.Helper()

	source, err := os.ReadFile(filepath.Join("..", "..", "..",
		"internal", "domain", "model", "project_model.go"))
	if err != nil {
		t.Fatalf("reading the project model: %v", err)
	}

	states := map[string]struct{}{}
	pattern := regexp.MustCompile(`ProjectStatus\w+\s*=\s*"([a-z]+)"`)

	for _, match := range pattern.FindAllSubmatch(source, -1) {
		states[string(match[1])] = struct{}{}
	}

	return states
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

// The small "delete" in a table row is a text button, not a filled one.
//
// button.link sets no background and the solid danger rule sets a red one at the
// same specificity, so source order decided it - and the solid rule came second.
// Every row's delete button turned into a red rectangle with red text in it: a
// coloured block with an invisible label, which is what a screenshot showed.
func TestTheSolidDangerButtonSparesTextButtons(t *testing.T) {
	css := asset(t, "/app.css")

	if !strings.Contains(css, "button.danger:not(.link)") {
		t.Error("the solid danger rule applies to .link buttons too, which paints the " +
			"row actions red on red")
	}

	// And the text-button rule is still there to colour them.
	if !strings.Contains(css, "button.link.danger") {
		t.Error("nothing colours the text of a destructive row action")
	}
}

// sheetCards reads the tables that have an export/import card out of app.js.
//
// The card is built by code, from this list, so the keys its words live under never
// appear as literals anywhere - the same blind spot the err.* sentences have. Read
// from the list rather than restated here, so adding a third table cannot pass with
// no words to show in it.
func sheetCards(t *testing.T) map[string]bool {
	t.Helper()

	js := asset(t, "/app.js")

	start := strings.Index(js, "const SHEET_CARDS = [")
	if start < 0 {
		t.Fatal("SHEET_CARDS is gone from app.js; the per-table spreadsheet cards changed " +
			"shape and this guard no longer guards anything")
	}

	end := strings.Index(js[start:], "\n];")
	if end < 0 {
		t.Fatal("SHEET_CARDS is not closed by \"\\n];\"; the guard cannot tell where the " +
			"list ends")
	}

	found := map[string]bool{}

	for _, match := range regexp.MustCompile(`key:\s*'([a-z]+)'`).
		FindAllStringSubmatch(js[start:start+end], -1) {
		found[match[1]] = true
	}

	if len(found) == 0 {
		t.Fatal("SHEET_CARDS lists no tables, so nothing can export or import")
	}

	return found
}

// Every table with a spreadsheet card has the words the card shows.
//
// Three keys per table, none of which appears as a literal: the card is built from
// the list, so a new table would silently show the English fallback to a German
// reader - and the fallback is the paragraph explaining what its import does, which
// is the one part somebody actually has to read.
func TestEverySheetCardIsNamed(t *testing.T) {
	dict, ok := dictionaries(t)["de"]
	if !ok {
		t.Fatal("no German dictionary")
	}

	// text is what the card says it does, file names the download, done reports
	// what was written.
	suffixes := []string{"text", "file", "done"}

	for table := range sheetCards(t) {
		for _, suffix := range suffixes {
			key := "sheet." + table + "." + suffix

			if _, translated := dict[key]; !translated {
				t.Errorf("the %s spreadsheet card has no German %q, so a German reader "+
					"is shown the English one", table, suffix)
			}
		}
	}

	// And nothing left over: a card that was removed leaves its paragraph behind,
	// and a paragraph nobody shows is a paragraph nobody notices is wrong.
	known := sheetCards(t)

	for key := range dict {
		rest, isCard := strings.CutPrefix(key, "sheet.")
		if !isCard {
			continue
		}

		table, suffix, cut := strings.Cut(rest, ".")
		if !cut || !known[table] {
			t.Errorf("%q belongs to no table in SHEET_CARDS", key)

			continue
		}

		if !slices.Contains(suffixes, suffix) {
			t.Errorf("%q is not one of the %v a card shows", key, suffixes)
		}
	}
}

// Selecting several rows to delete is derived from the rows themselves: the
// checkbox appears where a delete button already does, so the two cannot come to
// disagree about who may delete what.
//
// That only holds while every table asks deleteButton() for its delete button. A
// hand-rolled one looks identical on screen and is invisible to the column, so
// that table would quietly be the one without bulk deletion - and nobody would
// notice until they went looking for it.
func TestEveryRowDeletionGoesThroughTheSharedButton(t *testing.T) {
	js := asset(t, "/app.js")

	const built = "class: 'link danger'"

	if got := strings.Count(js, built); got != 1 {
		t.Errorf("%d places build a destructive row button by hand, want 1 (deleteButton); "+
			"a table that builds its own gets no checkbox column, because the column is "+
			"derived from the buttons", got)
	}

	// The derivation itself, in both directions: the button records what it would
	// delete, and the column reads it back.
	for _, needed := range []string{"button.deletes = { label, path, message, after }",
		"row.querySelectorAll('button.danger')", "box.deletes = deletes"} {
		if !strings.Contains(js, needed) {
			t.Errorf("app.js no longer contains %q, so the checkbox column and the delete "+
				"buttons are no longer the same decision", needed)
		}
	}
}
