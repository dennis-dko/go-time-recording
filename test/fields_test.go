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

// keptUnread is every unexported field nothing in its package reads, and why it
// is kept anyway. Keyed by type and field.
var keptUnread = map[string]string{}

// TestEveryUnexportedFieldIsRead fails on an unexported struct field that no
// production code in its own package reads.
//
// The shape this catches is a dependency handed to a constructor, stored, and
// never used, and nothing else here sees it: golangci-lint's unused check counts
// the composite literal that sets the field as a use, and deadcode follows
// functions rather than fields. There were six when this was written, every one
// a constructor demanding an argument its type never touched - and one of them,
// the time entries ProjectDomainService was given, the residue of a rule retired
// in b50276d whose doc comment still promised it.
//
// A read is a selector x.f that is not the target of a plain assignment, in a
// production file of the same package. It counts for every type with a field of
// that name, unless x is known to be a different type that does not embed it:
// the receiver of another type's method, or a parameter or variable declared as
// one. Both simpler rules were tried and both were blind. Matching by name alone
// let every other service's s.auth stand in for PasskeyService.auth; demanding
// that x be known as the type itself missed range variables, map lookups and
// nested selectors, which a parser cannot type. Reads in tests do not count,
// because a test fixture's own auth field masked the passkey service's - and a
// field only a test reads is state the program keeps and never uses.
func TestEveryUnexportedFieldIsRead(t *testing.T) {
	unread, examined := unreadFields(t)

	if examined < 200 {
		t.Fatalf("only %d unexported fields were examined; the walk has stopped "+
			"reading the tree", examined)
	}

	for _, key := range sortedKeys(unread) {
		if _, kept := keptUnread[key]; kept {
			continue
		}

		t.Errorf("%s (%s) is set and nothing in its package reads it. Delete it - "+
			"and the constructor parameter that fills it - or give it a row in "+
			"keptUnread saying what it is for", key, unread[key])
	}
}

// TestNoFieldIsKeptUnreadThatIsReadOrGone fails on a row in keptUnread whose
// field has since been read or removed.
func TestNoFieldIsKeptUnreadThatIsReadOrGone(t *testing.T) {
	unread, _ := unreadFields(t)

	for _, key := range sortedKeys(keptUnread) {
		if _, still := unread[key]; !still {
			t.Errorf("keptUnread keeps %s, which is read or gone now; delete the row", key)
		}
	}
}

// fieldRead is one selector: the field it names, and the type of the value it
// was read through where that is known.
type fieldRead struct {
	through string
	field   string
}

// unreadFields answers every unexported struct field under internal/ and cmd/
// that no production file of its package reads, keyed by type and field with
// where it is declared, and how many fields were examined.
func unreadFields(t *testing.T) (map[string]string, int) {
	t.Helper()

	root := ".."
	fset := token.NewFileSet()
	packages := map[string][]*ast.File{}
	names := map[*ast.File]string{}

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

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		rel = filepath.ToSlash(rel)

		production := strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go") &&
			(strings.HasPrefix(rel, "internal/") || strings.HasPrefix(rel, "cmd/"))
		if !production {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}

		dir := filepath.ToSlash(filepath.Dir(rel))
		packages[dir] = append(packages[dir], file)
		names[file] = rel

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	unread := map[string]string{}
	examined := 0

	for _, files := range packages {
		embeds := map[string]map[string]bool{}

		eachStruct(files, func(_ *ast.File, typ string, fields *ast.FieldList) {
			for _, field := range fields.List {
				if len(field.Names) == 0 {
					if embeds[typ] == nil {
						embeds[typ] = map[string]bool{}
					}

					embeds[typ][typeNameOf(field.Type)] = true
				}
			}
		})

		var reads []fieldRead

		for _, file := range files {
			for _, decl := range file.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
					reads = append(reads, readsIn(fn)...)
				}
			}
		}

		eachStruct(files, func(file *ast.File, typ string, fields *ast.FieldList) {
			for _, field := range fields.List {
				for _, name := range field.Names {
					if name.Name == "_" || ast.IsExported(name.Name) {
						continue
					}

					examined++

					if readSomewhere(reads, embeds, typ, name.Name) {
						continue
					}

					unread[typ+"."+name.Name] = names[file] + ":" +
						strconv.Itoa(fset.Position(name.Pos()).Line)
				}
			}
		})
	}

	return unread, examined
}

// eachStruct calls visit for every struct type declared in files.
func eachStruct(files []*ast.File, visit func(*ast.File, string, *ast.FieldList)) {
	for _, file := range files {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}

			for _, spec := range gen.Specs {
				ts := spec.(*ast.TypeSpec)
				if st, ok := ts.Type.(*ast.StructType); ok {
					visit(file, ts.Name.Name, st.Fields)
				}
			}
		}
	}
}

// readsIn returns every selector in a function that is not the target of a
// plain assignment, with the type of what it was read through where the
// function says so.
func readsIn(fn *ast.FuncDecl) []fieldRead {
	known := map[string]string{}

	if fn.Recv != nil {
		for _, recv := range fn.Recv.List {
			for _, name := range recv.Names {
				known[name.Name] = typeNameOf(recv.Type)
			}
		}
	}

	for _, param := range fn.Type.Params.List {
		for _, name := range param.Names {
			known[name.Name] = typeNameOf(param.Type)
		}
	}

	written := map[*ast.SelectorExpr]bool{}

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.ValueSpec:
			if node.Type != nil {
				for _, name := range node.Names {
					known[name.Name] = typeNameOf(node.Type)
				}
			}
		case *ast.AssignStmt:
			if node.Tok == token.ASSIGN {
				for _, lhs := range node.Lhs {
					if sel, ok := lhs.(*ast.SelectorExpr); ok {
						written[sel] = true
					}
				}
			}
		}

		return true
	})

	var reads []fieldRead

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || written[sel] {
			return true
		}

		through := ""
		if id, ok := sel.X.(*ast.Ident); ok {
			through = known[id.Name]
		}

		reads = append(reads, fieldRead{through: through, field: sel.Sel.Name})

		return true
	})

	return reads
}

// readSomewhere reports whether any read could be of field on typ.
func readSomewhere(reads []fieldRead, embeds map[string]map[string]bool, typ, field string) bool {
	for _, r := range reads {
		if r.field != field {
			continue
		}

		if r.through == "" || r.through == typ || embeds[r.through][typ] {
			return true
		}
	}

	return false
}

// typeNameOf is the name of a declared type, through a pointer or a type
// argument.
func typeNameOf(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return typeNameOf(t.X)
	case *ast.IndexExpr:
		return typeNameOf(t.X)
	}

	return ""
}
