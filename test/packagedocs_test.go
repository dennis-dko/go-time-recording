package test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Every package is described once, in a doc.go holding that description, its
// package clause and nothing else.
//
// Before this rule the description lived wherever the package happened to begin:
// in the file named after the package where there was one, and otherwise in the
// file carrying its subject - errors.go for rest, ldap.go for directory,
// visibility.go for the domain rules. Finding it meant knowing which. A file that
// was split or renamed took the description with it or left it in the wrong
// half, and a description of the whole package sat above the first declarations
// of one of its subjects, where it read as theirs. doc.go is the toolchain's own
// name for this file, so it is found without searching and nothing in it
// competes with the description.
//
// Asked of every directory holding Go files, packages made only of tests
// included, because they are described like any other and ST1000 skips them - as
// it skips every main package, which is why the opening words are checked here
// too: "Package <name>" for a library, "Command" for a program. doc.go declares
// and imports nothing and carries no other comment, a build constraint included,
// so go doc shows the description whichever suite's tag is set. And no other
// file carries a package comment: go doc appends every one it finds, so a second
// is a second description of the same thing, and two are how one goes stale.
func TestEveryPackageIsDescribedOnceInItsDocGo(t *testing.T) {
	root := ".."
	packages := map[string][]string{}

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			name := entry.Name()
			if path != root && (strings.HasPrefix(name, ".") || name == "bin" ||
				name == "node_modules" || name == "testdata") {
				return filepath.SkipDir
			}

			return nil
		}

		if strings.HasSuffix(path, ".go") {
			dir := filepath.Dir(path)
			packages[dir] = append(packages[dir], path)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("cannot walk the tree: %v", err)
	}

	// A walk that sees a fraction of the tree passes everything it did not see.
	if len(packages) < 30 {
		t.Fatalf("found %d package directories; the walk is not seeing the tree", len(packages))
	}

	dirs := make([]string, 0, len(packages))
	for dir := range packages {
		dirs = append(dirs, dir)
	}

	sort.Strings(dirs)

	for _, dir := range dirs {
		rel, err := filepath.Rel(root, dir)
		if err != nil {
			t.Fatalf("cannot place %s: %v", dir, err)
		}

		rel = filepath.ToSlash(rel)
		described := false

		for _, path := range packages[dir] {
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
			if err != nil {
				t.Fatalf("cannot parse %s: %v", path, err)
			}

			if filepath.Base(path) != "doc.go" {
				if file.Doc != nil {
					t.Errorf("%s/%s carries a package comment; %s is described in its doc.go alone",
						rel, filepath.Base(path), rel)
				}

				continue
			}

			described = true

			if file.Doc == nil {
				t.Errorf("%s/doc.go carries no package comment above its package clause", rel)

				continue
			}

			if len(file.Decls) > 0 {
				t.Errorf("%s/doc.go declares or imports something; it holds the description "+
					"and the package clause only", rel)
			}

			if len(file.Comments) != 1 {
				t.Errorf("%s/doc.go carries %d comments; the description is the only one it "+
					"holds, and a build constraint would hide it from go doc without that tag",
					rel, len(file.Comments))
			}

			opening := "Package " + file.Name.Name + " "
			if file.Name.Name == "main" {
				opening = "Command "
			}

			if !strings.HasPrefix(file.Doc.Text(), opening) {
				t.Errorf("%s/doc.go opens %q, want %q", rel, firstLine(file.Doc.Text()), opening+"...")
			}
		}

		if !described {
			t.Errorf("%s has no doc.go describing it", rel)
		}
	}
}

// firstLine is what a failure quotes of a description.
func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")

	return line
}
