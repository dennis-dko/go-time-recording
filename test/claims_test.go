package test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// CLAUDE.md names things, and until now only its line numbers were read back.
//
// TestCLAUDEmdStillPointsAtWhatItSaysItDoes covers the seven `file.go:N`
// citations, which is the sharpest kind of claim the document makes and also the
// narrowest: a hand-kept table of seven. Everything else the file asserts about
// the tree - the paths it names in passing, the packages its layout enumerates,
// the task targets it tells somebody to run - was unwatched, and none of it is
// visible to go vet, to staticcheck or to any suite, because the document is not
// compiled.
//
// The three cases below are the same idea widened, and they were taken from the
// directivecheck command in a sibling repository, where two claims had already
// gone stale before anything read them: a package the layout did not name, and a
// count that had moved from two to four while the sentence stating it had not.
// Nothing here is stale today - all three pass on the tree they were written
// against - so they are regression guards rather than a report of anything, and
// saying that plainly is part of what they are worth.
//
// Only claims with one mechanical answer are checked. The document also states
// things a program cannot settle - which remedy repairs a working copy, why the
// one permitted panic is permitted - and those stay unguarded on purpose. A check
// that guesses at prose is one whose output gets skimmed, and then the entries
// that are real go past unread.

// backticked matches the `...` spans the document uses for every path, command
// and identifier it names.
var backticked = regexp.MustCompile("`([^`]+)`")

// namedTask matches a task target the document tells somebody to run. The
// trailing alternation stops `task release VERSION=vX.Y.Z` from capturing the
// argument as part of the name.
var namedTask = regexp.MustCompile("`task ([a-z][a-z0-9:._-]*)(?:`| )")

// taskTarget matches a target declaration in the Taskfile, which is the only
// place a target is defined: two spaces, a name, a colon, end of line.
var taskTarget = regexp.MustCompile(`(?m)^  ([a-z][a-zA-Z0-9:._-]*):[ \t]*$`)

// fileSuffix lists the endings that make a backticked token a file rather than a
// directory, and it is the whole of that decision.
//
// The distinction is what makes this checkable at all. The document asserts that
// some paths exist and that others deliberately do not - there is no `adr/`, no
// `doc.go` anywhere in the tree, and `path` is imported nowhere - and telling an
// "exists" from a "deliberately does not" means reading the prose around it.
// Every one of those absence claims is a directory or a package. Reading file
// paths only makes that question disappear rather than answering it badly.
var fileSuffix = []string{
	".go", ".md", ".yml", ".yaml", ".json", ".js", ".css", ".html",
	".env", ".example", ".sh", ".conf", ".template", ".ldif", ".in", ".txt",
}

// ignoredPath is the one file the document names that is not in the tree, and
// that is the point of naming it: `deploy/.env` holds real passwords and is
// git-ignored, so a clone that has one has been configured and a clone that has
// not is correct.
//
// Listed here rather than resolved with `git check-ignore`, for two reasons. It
// keeps the case free of a dependency on git being on the PATH, and it makes the
// next addition a decision somebody writes down instead of a rule that quietly
// widens.
var ignoredPath = map[string]bool{
	"deploy/.env": true,
}

// TestCLAUDEmdNamesOnlyFilesThatAreThere checks every file path the document
// names against the tree.
func TestCLAUDEmdNamesOnlyFilesThatAreThere(t *testing.T) {
	root := ".."
	doc := read(t, filepath.Join(root, "CLAUDE.md"))

	tops := childDirs(t, root)

	var checked int

	for _, token := range namedPaths(doc, tops) {
		if ignoredPath[token] {
			continue
		}

		checked++

		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(token))); err != nil {
			t.Errorf("CLAUDE.md names %s and the tree has no such file. Either the "+
				"file moved and the sentence naming it did not, or the sentence is "+
				"describing something that no longer exists", token)
		}
	}

	// A path count that silently fell to zero would make this case pass while
	// reading nothing, which is the failure a document check is most prone to.
	// Twelve is what the document carries today once the one git-ignored path is
	// set aside; the floor is below that rather than at it, so ordinary editing
	// does not trip it and an extraction that has stopped working does.
	if checked < 8 {
		t.Fatalf("only %d file paths were found in CLAUDE.md; the extraction has "+
			"stopped matching the document rather than the document having emptied",
			checked)
	}
}

// TestCLAUDEmdNamesEveryPackageUnderInternal checks the layout from the other
// direction: not that what it names exists, but that what exists is named.
//
// Section 5 enumerates the tree, and an enumeration is a promise that it is
// complete. A package added without a line there is invisible to whoever reads
// the layout to find their way around, and that is exactly the reader the
// section is for.
func TestCLAUDEmdNamesEveryPackageUnderInternal(t *testing.T) {
	root := ".."
	doc := read(t, filepath.Join(root, "CLAUDE.md"))

	for name := range childDirs(t, filepath.Join(root, "internal")) {
		// The segment has to end where the name ends, so that internal/api is
		// not found inside internal/application.
		named := regexp.MustCompile("internal/" + regexp.QuoteMeta(name) + "[/`, )]").MatchString(doc)
		if !named {
			t.Errorf("internal/%s is a top-level package and the layout in CLAUDE.md "+
				"does not name it", name)
		}
	}
}

// TestCLAUDEmdNamesOnlyTasksThatExist checks the commands the document tells
// somebody to run.
//
// This is the claim most likely to rot without anybody noticing, because a
// renamed target fails only for whoever types the old name - and they will
// reasonably assume they mistyped it rather than that the document is wrong.
func TestCLAUDEmdNamesOnlyTasksThatExist(t *testing.T) {
	root := ".."
	doc := read(t, filepath.Join(root, "CLAUDE.md"))
	taskfile := read(t, filepath.Join(root, "Taskfile.yml"))

	targets := map[string]bool{}
	for _, m := range taskTarget.FindAllStringSubmatch(taskfile, -1) {
		targets[m[1]] = true
	}

	if len(targets) < 5 {
		t.Fatalf("only %d task targets were found in Taskfile.yml; the extraction "+
			"is reading the wrong shape", len(targets))
	}

	seen := map[string]bool{}

	for _, line := range strings.Split(doc, "\n") {
		for _, m := range namedTask.FindAllStringSubmatch(line, -1) {
			name := m[1]
			if seen[name] {
				continue
			}

			seen[name] = true

			if !targets[name] {
				t.Errorf("CLAUDE.md tells somebody to run `task %s` and Taskfile.yml "+
					"has no such target", name)
			}
		}
	}

	if len(seen) < 5 {
		t.Fatalf("only %d task names were found in CLAUDE.md; the extraction has "+
			"stopped matching the document", len(seen))
	}
}

// namedPaths returns every repository file CLAUDE.md names, in document order.
//
// tops is the set of top-level directories, and it is what separates this
// repository's own paths from the many that share their spelling: the standard
// library's path/filepath and net/http, a module path, a URL. Without it the
// case reports the standard library as missing from the tree.
func namedPaths(doc string, tops map[string]bool) []string {
	var out []string

	seen := map[string]bool{}

	for _, m := range backtickedPerLine(doc) {
		token := m
		if seen[token] || strings.ContainsAny(token, "<> ") || !strings.Contains(token, "/") {
			continue
		}

		// The document cites seven of these as `file.go:N`, so the line number
		// comes off before the suffix decides anything. Without this the citations
		// - the sharpest claims in the file - are the ones this reads past.
		if colon := strings.LastIndex(token, ":"); colon > 0 {
			if _, err := strconv.Atoi(token[colon+1:]); err == nil {
				token = token[:colon]
			}
		}

		if seen[token] || !tops[strings.SplitN(token, "/", 2)[0]] || !looksLikeFile(token) {
			continue
		}

		seen[token] = true

		out = append(out, token)
	}

	return out
}

// backtickedPerLine returns the contents of every `...` span, matched one line
// at a time.
//
// Per line rather than over the whole document, and that is not tidiness. The
// file contains fenced code blocks, so its backticks do not pair up when read
// straight through: everything after the first fence is matched against the
// wrong partner, and the extraction quietly returns half of what is there. Read
// whole, this found 6 of the document's 13 paths and reported nothing wrong,
// which is the shape of failure that makes a document check worse than none.
// Nothing here spans a line break, so the line is the right unit.
func backtickedPerLine(doc string) []string {
	var out []string

	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			continue
		}

		for _, m := range backticked.FindAllStringSubmatch(line, -1) {
			out = append(out, m[1])
		}
	}

	return out
}

// looksLikeFile reports whether a backticked token names a file rather than a
// directory.
func looksLikeFile(token string) bool {
	if token == "" || strings.HasSuffix(token, "/") {
		return false
	}

	for _, suffix := range fileSuffix {
		if strings.HasSuffix(token, suffix) {
			return true
		}
	}

	return false
}

// childDirs returns the names of the directories directly under path.
func childDirs(t *testing.T, path string) map[string]bool {
	t.Helper()

	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatalf("cannot read %s: %v", path, err)
	}

	out := map[string]bool{}

	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			out[entry.Name()] = true
		}
	}

	return out
}
