package web_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

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
