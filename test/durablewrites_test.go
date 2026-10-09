package test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// A file this application writes whole is forced to disk before anything uses it.
//
// Both such files are moved into place by a rename once written: the connection
// file over the old one, the downloaded binary over the running one. A rename is
// recorded as soon as the next journal commit, and the data it points at is not
// necessarily with it - on ext4 the rename that replaces an existing file waits
// for its data, and the one the binary swap makes, onto a name moved aside a
// moment before, does not. A power cut in that window leaves the name pointing at
// nothing: an empty binary the service manager cannot start, or an empty
// connection file, after which the next start serves the installer. Sync before
// the rename closes it, and no test can watch a power cut, so this holds the call
// in place instead.
//
// A file is recognised as written whole by how it is opened - os.CreateTemp, or
// os.OpenFile with O_TRUNC - and the function that opens it has to call Sync.
// Appending, and os.WriteFile's best-effort notes, are not this shape.
func TestAFileWrittenWholeReachesTheDiskBeforeItIsUsed(t *testing.T) {
	created := 0

	for _, root := range []string{"../internal", "../cmd"} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") ||
				strings.HasSuffix(path, "_test.go") {
				return err
			}

			fset := token.NewFileSet()

			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}

			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}

				var opened []token.Pos

				synced := false

				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}

					switch {
					case writesWhole(call):
						opened = append(opened, call.Pos())
					case calls(call, "Sync") && len(call.Args) == 0:
						synced = true
					}

					return true
				})

				created += len(opened)

				if len(opened) > 0 && !synced {
					for _, at := range opened {
						t.Errorf("%s writes a file whole in %s and never forces it to disk; "+
							"call Sync before it is closed and moved into place",
							fset.Position(at), fn.Name.Name)
					}
				}
			}

			return nil
		})
		if err != nil {
			t.Fatalf("reading %s: %v", root, err)
		}
	}

	// The connection file and the download. Fewer means the walk has stopped
	// recognising them, and this would pass while checking nothing.
	if created < 2 {
		t.Fatalf("only %d files written whole were found; the shape this looks for "+
			"has stopped matching the tree", created)
	}
}

// writesWhole reports a call that opens a file to write it from the beginning.
func writesWhole(call *ast.CallExpr) bool {
	if isPackageCall(call, "os", "CreateTemp") {
		return true
	}

	if !isPackageCall(call, "os", "OpenFile") || len(call.Args) < 2 {
		return false
	}

	found := false

	ast.Inspect(call.Args[1], func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "O_TRUNC" {
			found = true
		}

		return !found
	})

	return found
}

// isPackageCall reports a call to pkg.name.
func isPackageCall(call *ast.CallExpr, pkg, name string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}

	ident, ok := sel.X.(*ast.Ident)

	return ok && ident.Name == pkg
}

// calls reports a method call by that name on anything.
func calls(call *ast.CallExpr, method string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)

	return ok && sel.Sel.Name == method
}
