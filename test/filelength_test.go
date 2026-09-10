package test

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// lengthBudget is where a file here stops being ordinary.
//
// Measured on this tree rather than borrowed: a production Go file has a median
// of 138 lines and a ninetieth percentile of 449. The fault this guards against
// is not length but a file that has stopped describing itself - test/browser's
// admin file reached 5,744 lines while its first sentence still said it covered
// three things - and length is only how that gets noticed in time.
const lengthBudget = 800

// overBudget is every file allowed past the budget, and why.
//
// The reason is the row. Where the honest one is a split nobody has made yet, it
// says so rather than inventing a justification: a list that is wrong about why
// a file is long is worse than the file, because it is read instead of the file.
var overBudget = map[string]string{
	"internal/infrastructure/persistence/migrations/migrations.go": "an append-only chain whose order is " +
		"its content; a second file would hold half of that order and neither half would show it",
	"cmd/main.go": "start-up ordering is the subject and it reads top to bottom; splitting it is the one " +
		"change that would hide the order, which is why the logic-read ledger reads it whole",
	"test/harness/harness.go": "one object shared by three suites - building the binary, giving an " +
		"instance its database and port, knowing when it is ready; creating the database is the part " +
		"that could stand alone",
	"internal/infrastructure/selfupdate/selfupdate_test.go": "a few lines over, and one subject: the " +
		"updater's own cases beside the one file they test",
	"internal/interface/web/assets/app.js": "one script with no build step to split it; read by its " +
		"section banners instead, on the rotation CLAUDE.md keeps",
	"internal/interface/web/assets/app.css": "one stylesheet for one page, kept in the order of the " +
		"script's sections",
	"internal/interface/web/assets/index.html": "the one document of a single-page application; every " +
		"view is a section of it",
}

// TestNoFileGrowsPastTheBudgetWithoutADecision fails on a file over the budget
// that no row names.
//
// The failure is the moment the question gets asked, which is the whole value:
// asked at 801 lines it is a small split, asked at 5,744 it is an afternoon.
func TestNoFileGrowsPastTheBudgetWithoutADecision(t *testing.T) {
	files := measuredFiles(t)

	// A walk that silently read nothing would pass this case while checking
	// nothing. The tree has several hundred such files; the floor sits well below
	// that, so ordinary deletions do not trip it and a broken walk does.
	if len(files) < 250 {
		t.Fatalf("only %d files were measured; the walk has stopped finding the tree", len(files))
	}

	for _, path := range sortedKeys(files) {
		lines := files[path]
		if lines <= lengthBudget {
			continue
		}

		if _, excused := overBudget[path]; excused {
			continue
		}

		t.Errorf("%s is %d lines, past the budget of %d. Split it by subject, shorten it, "+
			"or give it a row in overBudget saying why it stays - CLAUDE.md, Section 2",
			path, lines, lengthBudget)
	}
}

// TestNoExemptionOutlivesTheLengthItExcused fails on a row whose file is gone
// or has come back under the budget.
//
// Without it the list only grows, and a list of exemptions nobody needs is one
// nobody reads - which is how the row that matters gets waved through with the
// rest.
func TestNoExemptionOutlivesTheLengthItExcused(t *testing.T) {
	files := measuredFiles(t)

	for _, path := range sortedKeys(overBudget) {
		lines, present := files[path]

		switch {
		case !present:
			t.Errorf("overBudget excuses %s, which is not in the tree; delete the row", path)
		case lines <= lengthBudget:
			t.Errorf("overBudget excuses %s, which is %d lines and within the budget of %d now; "+
				"delete the row", path, lines, lengthBudget)
		}

		if strings.TrimSpace(overBudget[path]) == "" {
			t.Errorf("the row for %s gives no reason, and the reason is what the row is for", path)
		}
	}
}

// measuredFiles returns the line count of every file the budget applies to,
// keyed by its slash path from the repository root.
//
// Every Go file, whatever its build tag, and the three languages the interface
// is written in. The OpenAPI description is left out: it is read by a program,
// not by a person in one sitting.
func measuredFiles(t *testing.T) map[string]int {
	t.Helper()

	root := ".."
	out := map[string]int{}

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			// bin holds Task's temporary build directory; the dot directories are
			// git's and the editor's.
			name := entry.Name()
			if path != root && (strings.HasPrefix(name, ".") || name == "bin" || name == "node_modules") {
				return filepath.SkipDir
			}

			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		rel = filepath.ToSlash(rel)
		if !underBudget(rel) {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		// Newlines, so the count is the one wc -l prints and the one the rule
		// was measured with.
		out[rel] = bytes.Count(data, []byte("\n"))

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return out
}

// underBudget reports whether the budget applies to a file.
func underBudget(rel string) bool {
	if strings.HasSuffix(rel, ".go") {
		return true
	}

	if !strings.HasPrefix(rel, "internal/interface/web/assets/") {
		return false
	}

	return strings.HasSuffix(rel, ".js") || strings.HasSuffix(rel, ".css") ||
		strings.HasSuffix(rel, ".html")
}
