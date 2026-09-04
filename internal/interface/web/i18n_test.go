package web_test

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
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

// Why one row of an imported file cannot be written, in the reader's language.
//
// The preview these land in translates everything else about itself - the
// headings, and the cells, down to writing a status as the file wrote it - and
// this one column was English prose on the grounds that what is wrong with row 47
// of somebody's file is not a fixed set of reasons. It is a fixed set of reasons:
// they are all in the spreadsheet reader and the two import planners, and this is
// what keeps them all answered.
func TestEveryImportRowProblemIsTranslated(t *testing.T) {
	codes := importRowProblemCodes(t)
	if len(codes) == 0 {
		t.Fatal("no row problem codes found; this test is no longer reading the source")
	}

	dict, ok := dictionaries(t)["de"]
	if !ok {
		t.Fatal("app.js has no German dictionary")
	}

	var untranslated, orphaned []string

	for code := range codes {
		if _, found := dict["row."+code]; !found {
			untranslated = append(untranslated, code)
		}
	}

	for key := range dict {
		code, isRow := strings.CutPrefix(key, "row.")
		if !isRow {
			continue
		}

		if _, found := codes[code]; !found {
			orphaned = append(orphaned, key)
		}
	}

	sort.Strings(untranslated)
	sort.Strings(orphaned)

	if len(untranslated) > 0 {
		t.Errorf("%d row problem(s) have no German sentence, so a German reader is "+
			"shown English beside German columns: %v", len(untranslated), untranslated)
	}

	if len(orphaned) > 0 {
		t.Errorf("%d German sentence(s) are for row problems nothing reports: %v",
			len(orphaned), orphaned)
	}
}

// importRowProblemCodes collects the codes the row complaints are built with.
//
// Three spellings, because the complaint is made in three places: the reader of
// the workbook, which knows a date is not a date; the planners, which know a
// project cannot be archived yet; and the time-entry planner, which has a helper
// of its own from before any of this had codes.
func importRowProblemCodes(t *testing.T) map[string]struct{} {
	t.Helper()

	codes := map[string]struct{}{}
	pattern := regexp.MustCompile(`(?:\bproblemf|\bProblemf|\brefuse)\("([^"]+)"`)

	root := filepath.Join("..", "..", "..", "internal")

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
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

		// The doc comments on Problemf and problemf describe them rather than call
		// them, and a comment showing a code would register as one.
		for _, match := range pattern.FindAllSubmatch(withoutGoComments(source), -1) {
			codes[string(match[1])] = struct{}{}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walking the source: %v", err)
	}

	return codes
}

// withoutGoComments drops // comments so a line describing a call is not read as
// one.
func withoutGoComments(source []byte) []byte {
	lines := strings.Split(string(source), "\n")

	for i, line := range lines {
		if before, _, ok := strings.Cut(line, "//"); ok {
			lines[i] = before
		}
	}

	return []byte(strings.Join(lines, "\n"))
}

// serverErrorCodes collects the codes attached with WithCode across the Go source.
func serverErrorCodes(t *testing.T) map[string]struct{} {
	t.Helper()

	// From this package up to the module root, which is where internal/ lives.
	root := filepath.Join("..", "..", "..")
	codes := map[string]struct{}{}
	// Two ways a code is declared, and both have to be seen or this test reports
	// the other one as an orphan. WithCode names a rule where it is enforced;
	// the constants in apperror name the generic reasons that are not rules at
	// all - an internal failure, a connection that did not get through - and
	// those are shared by everything that can hit them.
	pattern := regexp.MustCompile(`WithCode\("([^"]+)"|Code[A-Z]\w*\s*=\s*"([^"]+)"`)

	err := filepath.WalkDir(filepath.Join(root, "internal"),
		func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if entry.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}

			// Tests invent codes as fixtures. One of them named a code the server
			// does not send, and this reported it as a translation somebody had
			// forgotten to write - which is the opposite of what it means.
			if strings.HasSuffix(path, "_test.go") {
				return nil
			}

			// The doc comment on WithCode itself shows an example, which is not a
			// call site and would otherwise register as one.
			if strings.HasSuffix(path, filepath.Join("apperror", "apperror.go")) {
				return nil
			}

			// The installer is a different screen with a different dictionary. It
			// runs before there is a database to ask anything, so it carries its
			// own translations inside its one self-contained page - and the codes
			// it sends are shown there and nowhere else. Sweeping them up here
			// asked app.js for a sentence it would never have a use for.
			// TestTheInstallerTranslatesItsOwnRefusals is the guard for those.
			if strings.Contains(path, filepath.Join("interface", "installer")) {
				return nil
			}

			source, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}

			for _, match := range pattern.FindAllSubmatch(source, -1) {
				// Whichever of the two alternatives matched. A WithCode call fills
				// the first group and a constant declaration the second.
				for _, group := range match[1:] {
					if len(group) > 0 {
						codes[string(group)] = struct{}{}
					}
				}
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

// Only reads are given up on.
//
// Aborting a request in the browser does not stop the server, which finishes the
// work either way. A timeout on a write would therefore report a failure for
// something that succeeded, and the obvious response to that message is to do it
// again - which for an import means writing every row twice.
//
// The distinction lives in one expression, and nothing about it is self-evident
// from reading the call: a later hand tidying "why is this only for safe methods"
// would take the guard rails off a lorry.
func TestOnlyReadRequestsAreGivenUpOn(t *testing.T) {
	js := asset(t, "/app.js")

	const gate = "SAFE_METHODS.has(method) && !options.signal ? new AbortController() : null"

	if !strings.Contains(js, gate) {
		t.Error("the request timeout is no longer limited to safe methods and to calls " +
			"that brought no signal of their own; a write aborted here reports a " +
			"failure for work the server has already done")
	}

	// The timer has to be cleared however the request ends, or a fast one leaves a
	// pending abort behind for a controller nobody is waiting on any more.
	if !strings.Contains(js, "clearTimeout(countdown)") {
		t.Error("the timeout timer is not cleared, so every request leaves one pending")
	}

	// And only our own abort may be reported as a timeout: a caller's signal firing
	// is their business, and "the server did not answer" would be a lie about it.
	if !strings.Contains(js, "if (giveUp?.signal.aborted)") {
		t.Error("any abort is now reported as a timeout, including one a caller asked " +
			"for itself")
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

// The interface reads the field names the server actually sends.
//
// This is not a translation check, but it lives with them because it is the same
// class of mistake: a name that has to match something outside the file, written
// from memory. The evaluation's charts read project.project and day.booked -
// neither of which exists - so every label came out as "no project" and the
// per-day value was undefined, which threw inside the formatter and left an
// empty chart behind an error toast. Nothing failed loudly, and no test noticed.
func TestTheChartsReadFieldsTheStatisticsEndpointSends(t *testing.T) {
	js := asset(t, "/app.js")

	// The names on the wire, taken from the response type rather than repeated
	// here: a rename there has to show up as a failure here.
	source := readSource(t, filepath.Join("..", "api", "v1", "rest", "statistics_handler.go"))

	for _, field := range []string{"date", "hours", "name", "projectId"} {
		if !strings.Contains(source, `json:"`+field+`"`) {
			t.Fatalf("statistics_handler.go no longer sends %q; this test is out of date", field)
		}
	}

	// Comments stripped first, and word boundaries on the names. The prose above
	// the fixed code names the wrong fields on purpose, and project.projectId is
	// a real field that starts with one of them - both would otherwise read as
	// the bug they describe.
	code := withoutLineComments(js)

	for _, wrong := range []string{"day.booked", "project.project", "day.total", "project.total"} {
		if regexp.MustCompile(regexp.QuoteMeta(wrong) + `\b`).MatchString(code) {
			t.Errorf("a chart reads %s, which the statistics endpoint does not send", wrong)
		}
	}

	// And the two the report chart depends on, so a rename cannot quietly break
	// only the screen no Go test covers.
	for _, needed := range []string{"day.hours", "project.hours", "project.name"} {
		if !strings.Contains(js, needed) {
			t.Errorf("no chart reads %s any more; the report chart needs it", needed)
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

// A field's maxlength is what the server enforces, and not only what the form
// suggests.
//
// Every one of these was a number in the markup alone. The API took whatever it
// was sent, which makes a maxlength a hint to whoever fills in the form and no
// limit at all to whoever calls the endpoint - and the title and the banner are
// read by everybody who opens the sign-in page, before there is a session.
//
// Both directions matter. A markup limit above the server's is a form that lets
// somebody type a title, press Save and be told the title is invalid; one below
// it is a limit nobody can reach, which is the kind of number that stays wrong
// because nothing ever trips over it.
func TestTheFormLimitsAreTheOnesTheServerEnforces(t *testing.T) {
	html := asset(t, "/")

	for field, limit := range map[string]int{
		"title":       model.MaxTitleLength,
		"tabTitle":    model.MaxTabTitleLength,
		"banner":      model.MaxBannerLength,
		"footerText":  model.MaxFooterTextLength,
		"legalNotice": model.MaxLegalNoticeLength,
		"companyName": model.MaxCompanyNameLength,
		"message":     model.MaintenanceMessageLimit,

		// The account form bounded the name and not the address beside it, and
		// the server bounds both - so a long name was caught while typing and a
		// long address only on pressing Save.
		"email": model.MaxEmailLength,
	} {
		t.Run(field, func(t *testing.T) {
			pattern := regexp.MustCompile(
				`name="` + regexp.QuoteMeta(field) + `"[^>]*maxlength="(\d+)"`)

			match := pattern.FindStringSubmatch(html)
			if match == nil {
				t.Fatalf("the %s field has no maxlength, so the form offers to type "+
					"what the server will refuse", field)
			}

			written, err := strconv.Atoi(match[1])
			if err != nil {
				t.Fatalf("maxlength %q is not a number", match[1])
			}

			if written != limit {
				t.Errorf("the form allows %d characters and the server allows %d",
					written, limit)
			}
		})
	}
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

// Every right this application enforces is named in words and explained.
//
// The ticking boxes used to say "timesheets:write:own", which asks somebody
// deciding what a colleague may do to read a namespace. Words instead - and a
// right added later without them falls back to the identifier, which is the old
// screen again for that one line.
//
// Both directions: a name for a right that no longer exists is a translation
// nobody will ever see, and the way a dictionary rots.
func TestEveryPermissionIsNamedAndExplained(t *testing.T) {
	rights := permissionRights(t)
	if len(rights) == 0 {
		t.Fatal("no permissions found; this test is no longer reading the model")
	}

	dict, ok := dictionaries(t)["de"]
	if !ok {
		t.Fatal("app.js has no German dictionary")
	}

	var unnamed, unexplained []string

	for right := range rights {
		if _, found := dict["perm."+right]; !found {
			unnamed = append(unnamed, right)
		}

		if _, found := dict["perm.desc."+right]; !found {
			unexplained = append(unexplained, right)
		}
	}

	sort.Strings(unnamed)
	sort.Strings(unexplained)

	if len(unnamed) > 0 {
		t.Errorf("%d right(s) have no German name, so the box shows the identifier: %v",
			len(unnamed), unnamed)
	}

	if len(unexplained) > 0 {
		t.Errorf("%d right(s) are missing from the legend: %v",
			len(unexplained), unexplained)
	}

	// And the areas they are grouped under.
	for area := range permissionAreas(t) {
		if _, found := dict["perm.group."+area]; !found {
			t.Errorf("the %q group has no German heading", area)
		}
	}

	// The other direction.
	for key := range dict {
		rest, isPerm := strings.CutPrefix(key, "perm.")
		if !isPerm || rest == "legend" {
			continue
		}

		if area, isGroup := strings.CutPrefix(rest, "group."); isGroup {
			if _, known := permissionAreas(t)[area]; !known {
				t.Errorf("%q names an area no right belongs to", key)
			}

			continue
		}

		right := strings.TrimPrefix(rest, "desc.")

		if _, known := rights[right]; !known {
			t.Errorf("%q describes a right this application does not enforce", key)
		}
	}
}

// permissionRights is every right the model declares.
func permissionRights(t *testing.T) map[string]struct{} {
	t.Helper()

	source, err := os.ReadFile(filepath.Join("..", "..", "..",
		"internal", "domain", "model", "permission_model.go"))
	if err != nil {
		t.Fatalf("reading the permission model: %v", err)
	}

	rights := map[string]struct{}{}
	pattern := regexp.MustCompile(`Perm\w+\s*=\s*"([a-z:]+)"`)

	for _, match := range pattern.FindAllSubmatch(source, -1) {
		rights[string(match[1])] = struct{}{}
	}

	return rights
}

// permissionAreas is the first part of every right, which is what the boxes are
// grouped under.
func permissionAreas(t *testing.T) map[string]struct{} {
	t.Helper()

	areas := map[string]struct{}{}

	for right := range permissionRights(t) {
		area, _, _ := strings.Cut(right, ":")
		areas[area] = struct{}{}
	}

	return areas
}
