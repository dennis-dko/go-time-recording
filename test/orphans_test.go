package test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// unreadOnPurpose is every package-level constant or variable nothing reads,
// and why it is kept anyway. Keyed by package name and identifier.
var unreadOnPurpose = map[string]string{}

// TestEveryPackageLevelConstantAndVariableIsRead fails on a constant or a
// variable in production code that nothing in the repository reads.
//
// The linter does not ask this for a constant in a parenthesised group, and
// that was measured rather than inferred: an unused const, var, func and type
// on their own are all reported by golangci-lint's unused check, a var inside a
// group whose sibling is used is still reported - and a const inside such a
// group is not, because staticcheck treats a const group as one unit. Nothing
// asks it for an exported one either. defaultAutoCloseAfterDays sat in the
// configuration package for exactly that reason, naming a fourteen-day
// auto-close this application has never had, in a package the linter called
// "0 issues".
//
// A use is the identifier appearing anywhere else in any Go file, tests and the
// tagged suites included, since go/parser ignores build constraints. Matching by
// name rather than by resolved object means a same-named identifier in another
// package counts as a use, so this can miss an orphan but cannot invent one -
// the direction a check that fails the build has to err in.
func TestEveryPackageLevelConstantAndVariableIsRead(t *testing.T) {
	declared, uses := packageLevelValues(t)

	if len(declared) < 150 {
		t.Fatalf("only %d package-level constants and variables were found; the "+
			"walk has stopped reading the tree", len(declared))
	}

	for _, key := range sortedKeys(declared) {
		if uses[declared[key].name] > 0 {
			continue
		}

		if _, kept := unreadOnPurpose[key]; kept {
			continue
		}

		t.Errorf("%s (%s) is declared and nothing reads it. Delete it, or give it a row "+
			"in unreadOnPurpose saying what it is for", key, declared[key].at)
	}
}

// TestNothingIsKeptUnreadThatIsReadOrGone fails on a row in unreadOnPurpose
// whose identifier has since been read or removed, so the list cannot outlive
// what it excused.
func TestNothingIsKeptUnreadThatIsReadOrGone(t *testing.T) {
	declared, uses := packageLevelValues(t)

	for _, key := range sortedKeys(unreadOnPurpose) {
		d, present := declared[key]

		switch {
		case !present:
			t.Errorf("unreadOnPurpose keeps %s, which is no longer declared; delete the row", key)
		case uses[d.name] > 0:
			t.Errorf("unreadOnPurpose keeps %s, which something reads now; delete the row", key)
		}
	}
}

// packageValue is one package-level constant or variable, and where it is.
type packageValue struct {
	name string
	at   string
}

// packageLevelValues parses every Go file in the repository and answers the
// package-level constants and variables of the production code, keyed by
// package and name, together with how often each identifier is used anywhere.
func packageLevelValues(t *testing.T) (map[string]packageValue, map[string]int) {
	t.Helper()

	root := ".."
	fset := token.NewFileSet()

	type parsed struct {
		rel  string
		file *ast.File
	}

	var files []parsed

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			name := entry.Name()
			if path != root && (strings.HasPrefix(name, ".") || name == "bin" || name == "node_modules") {
				return filepath.SkipDir
			}

			return nil
		}

		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		files = append(files, parsed{rel: filepath.ToSlash(rel), file: file})

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	declared := map[string]packageValue{}
	declaredAt := map[token.Pos]bool{}

	for _, f := range files {
		production := !strings.HasSuffix(f.rel, "_test.go") &&
			(strings.HasPrefix(f.rel, "internal/") || strings.HasPrefix(f.rel, "cmd/"))
		if !production {
			continue
		}

		for _, decl := range f.file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || (gen.Tok != token.CONST && gen.Tok != token.VAR) {
				continue
			}

			for _, spec := range gen.Specs {
				for _, name := range spec.(*ast.ValueSpec).Names {
					if name.Name == "_" {
						continue
					}

					declaredAt[name.Pos()] = true
					declared[f.file.Name.Name+"."+name.Name] = packageValue{
						name: name.Name,
						at:   f.rel + ":" + strconv.Itoa(fset.Position(name.Pos()).Line),
					}
				}
			}
		}
	}

	uses := map[string]int{}

	for _, f := range files {
		ast.Inspect(f.file, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && !declaredAt[id.Pos()] {
				uses[id.Name]++
			}

			return true
		})
	}

	return declared, uses
}
