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

// Every fmt.Errorf that carries an error wraps it.
//
// CLAUDE.md says so, and its sweep for the opposite was recorded as finding
// none while one sat in selfupdate: its format string ran over two lines, so the
// line that held %v was not the line that held fmt.Errorf, and a grep that reads
// one line at a time could not see it. This parses instead, joining a format
// written as a concatenation the way the compiler does.
//
// An error is recognised by its name - err, anything ending in err, or a call to
// Err() - which is how this tree names them; one held under another name would
// pass unseen.
func TestEveryErrorfWrapsTheErrorItCarries(t *testing.T) {
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

			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || !isErrorf(call) || len(call.Args) < 2 {
					return true
				}

				format, ok := constantString(call.Args[0])
				if !ok || strings.Contains(format, "%w") {
					return true
				}

				for _, arg := range call.Args[1:] {
					if namedLikeAnError(arg) {
						t.Errorf("%s formats an error without %%w: %q", fset.Position(call.Pos()), format)

						break
					}
				}

				return true
			})

			return nil
		})
		if err != nil {
			t.Fatalf("reading %s: %v", root, err)
		}
	}
}

func isErrorf(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Errorf" {
		return false
	}

	pkg, ok := sel.X.(*ast.Ident)

	return ok && pkg.Name == "fmt"
}

// constantString is the value of a string literal, or of literals joined with +.
func constantString(expr ast.Expr) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}

		value, err := strconv.Unquote(e.Value)

		return value, err == nil
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}

		left, okLeft := constantString(e.X)
		right, okRight := constantString(e.Y)

		return left + right, okLeft && okRight
	case *ast.ParenExpr:
		return constantString(e.X)
	}

	return "", false
}

func namedLikeAnError(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.Ident:
		return strings.HasSuffix(strings.ToLower(e.Name), "err")
	case *ast.SelectorExpr:
		return strings.HasSuffix(strings.ToLower(e.Sel.Name), "err")
	case *ast.CallExpr:
		sel, ok := e.Fun.(*ast.SelectorExpr)

		return ok && sel.Sel.Name == "Err"
	}

	return false
}
