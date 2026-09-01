package test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// The two invariants CLAUDE.md states as measured facts, measured.
//
// Both were written down as counts - "zero violations of either, so a hit is a
// regression and not a backlog item", and "measured, internal has exactly one
// violation" - and both were re-measured by hand at each audit, from a grep in a
// markdown file. A count nobody recomputes is a claim, and the audit that
// recomputes it spends its attention on arithmetic rather than on reading.
//
// Parsed rather than grepped, and that matters for one of them in particular:
// go/parser reads every file whatever its build tags, so restart_other.go is
// examined on this machine even though nothing here ever compiles it. A grep or
// a vet run on one platform is blind to the other's file, which is the exact
// arrangement in which two copies drift - it is why selfupdate's two swap files
// held the same body unnoticed.
func TestNothingStuttersAndTheLayeringHolds(t *testing.T) {
	packages := parseInternal(t)

	t.Run("layering", func(t *testing.T) {
		// The dependency direction, as CLAUDE.md's own grep states it: the domain
		// depends on nothing further out, and the application layer does not reach
		// back into the interface. Enforced here rather than described, because
		// this is the invariant whose breach is architectural rather than
		// cosmetic - a service that imports a handler is a service that can no
		// longer be tested or reused without one.
		forbidden := map[string][]string{
			"internal/domain": {
				"go-time-recording/internal/infrastructure",
				"go-time-recording/internal/interface",
			},
			"internal/application": {
				"go-time-recording/internal/interface",
			},
		}

		for _, pkg := range packages {
			for layer, banned := range forbidden {
				if !strings.HasPrefix(pkg.dir, layer) {
					continue
				}

				for _, imported := range pkg.imports {
					for _, ban := range banned {
						if strings.Contains(imported.path, ban) {
							t.Errorf("%s:%d: %s imports %s, which points the wrong way "+
								"through the layers", imported.file, imported.line,
								pkg.dir, imported.path)
						}
					}
				}
			}
		}
	})

	t.Run("stuttering", func(t *testing.T) {
		// rest.RestHandler rather than rest.Handler. The rule holds here and the
		// tree satisfies it, which is only worth saying because four of the five
		// hits a bare-identifier scan returns are prefix accidents:
		// announce.Announcement, rest.Restart, rest.RestartHandler,
		// rest.RestartResponse. Requiring the character after the package name to
		// be upper case is what tells "Announce" + "ment" from "Rest" + "Handler",
		// and it is why this is a check rather than a list of exceptions.
		var found []string

		for _, pkg := range packages {
			prefix := strings.ToUpper(pkg.name[:1]) + pkg.name[1:]

			for _, decl := range pkg.exported {
				rest, cut := strings.CutPrefix(decl.name, prefix)
				if !cut || rest == "" {
					continue
				}

				if unicode.IsUpper(rune(rest[0])) {
					found = append(found, decl.file+":"+strconv.Itoa(decl.line)+
						": "+pkg.name+"."+decl.name+" repeats its package name; "+
						pkg.name+"."+rest+" says the same thing")
				}
			}
		}

		sort.Strings(found)

		for _, one := range found {
			t.Error(one)
		}
	})
}

type declaration struct {
	name string
	file string
	line int
}

type importOf struct {
	path string
	file string
	line int
}

type parsedPackage struct {
	// name is the package clause; dir is its path from the repository root, in
	// slashes, because that is how the layers are written down.
	name string
	dir  string

	exported []declaration
	imports  []importOf
}

// parseInternal reads every package under internal/, tests excluded.
//
// Tests are excluded for the layering check's sake rather than the naming one:
// CLAUDE.md counts violations "outside _test.go", because a test may legitimately
// reach across layers to assemble the thing it is testing.
func parseInternal(t *testing.T) []parsedPackage {
	t.Helper()

	root := filepath.Join("..", "internal")
	fset := token.NewFileSet()

	byDir := map[string]*parsedPackage{}

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly|parser.SkipObjectResolution)
		if err != nil {
			return err
		}

		// Parsed twice on purpose: ImportsOnly is enough for the layering half and
		// stops at the first declaration, so the naming half needs the whole file.
		full, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}

		dir := filepath.ToSlash(filepath.Dir(strings.TrimPrefix(
			filepath.ToSlash(path), "../")))

		pkg, seen := byDir[dir]
		if !seen {
			pkg = &parsedPackage{name: full.Name.Name, dir: dir}
			byDir[dir] = pkg
		}

		shown := filepath.ToSlash(strings.TrimPrefix(filepath.ToSlash(path), "../"))

		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				continue
			}

			pkg.imports = append(pkg.imports, importOf{
				path: path, file: shown, line: fset.Position(spec.Pos()).Line,
			})
		}

		pkg.exported = append(pkg.exported, exportedIn(full, fset, shown)...)

		return nil
	})
	if err != nil {
		t.Fatalf("reading internal/: %v", err)
	}

	if len(byDir) == 0 {
		t.Fatal("no packages found under internal/; this test is reading nothing")
	}

	out := make([]parsedPackage, 0, len(byDir))
	for _, pkg := range byDir {
		out = append(out, *pkg)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].dir < out[j].dir })

	return out
}

// exportedIn collects the package-level exported names.
//
// Methods are deliberately not among them: a method is read as
// receiver.Method, never as package.Method, so it cannot stutter with the
// package name however it is spelled.
func exportedIn(file *ast.File, fset *token.FileSet, shown string) []declaration {
	var out []declaration

	add := func(name *ast.Ident) {
		if name == nil || !name.IsExported() {
			return
		}

		out = append(out, declaration{
			name: name.Name, file: shown, line: fset.Position(name.Pos()).Line,
		})
	}

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil {
				add(d.Name)
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					add(s.Name)
				case *ast.ValueSpec:
					for _, name := range s.Names {
						add(name)
					}
				}
			}
		}
	}

	return out
}
