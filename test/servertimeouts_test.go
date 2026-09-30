package test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Every server this application runs lets go of a connection that has gone quiet.
//
// net/http waits for the next request on a kept-alive connection for IdleTimeout,
// or ReadTimeout when that is zero - and with both zero it clears the deadline and
// waits for ever (server.go, where it calls SetReadDeadline(time.Time{}) before
// peeking at the next request). ReadHeaderTimeout does not cover that wait: it
// starts only once the first bytes of the next request arrive.
//
// The HTTPS front end had an IdleTimeout. The plain-HTTP redirect beside it, on
// port 80 and open to anybody, did not, and neither did the installer, which
// answers anybody before a database exists and for as long as nobody answers
// it. One request each, then silence, was a connection and a goroutine held
// until the process ended, as many times as a visitor liked.
func TestEveryServerLetsGoOfAQuietConnection(t *testing.T) {
	var found int

	fset := token.NewFileSet()

	for _, root := range []string{"internal", "cmd"} {
		for _, path := range goFilesAndTestsUnder(t, filepath.Join("..", root)) {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}

			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatalf("parsing %s: %v", path, err)
			}

			ast.Inspect(file, func(n ast.Node) bool {
				literal, ok := n.(*ast.CompositeLit)
				if !ok || !isHTTPServer(literal.Type) {
					return true
				}

				found++

				set := map[string]bool{}

				for _, element := range literal.Elts {
					if kv, ok := element.(*ast.KeyValueExpr); ok {
						if key, ok := kv.Key.(*ast.Ident); ok {
							set[key.Name] = true
						}
					}
				}

				at := filepath.ToSlash(strings.TrimPrefix(filepath.ToSlash(path), "../")) + ":" +
					strconv.Itoa(fset.Position(literal.Pos()).Line)

				if !set["ReadHeaderTimeout"] {
					t.Errorf("%s: an http.Server with no ReadHeaderTimeout waits for ever on "+
						"a client that sends its headers slowly", at)
				}

				if !set["IdleTimeout"] && !set["ReadTimeout"] {
					t.Errorf("%s: an http.Server with neither IdleTimeout nor ReadTimeout "+
						"holds a kept-alive connection open for ever between requests", at)
				}

				return true
			})
		}
	}

	if found == 0 {
		t.Fatal("no http.Server was found at all; this test is reading nothing")
	}
}

// isHTTPServer reports whether a composite literal's type is http.Server.
func isHTTPServer(expr ast.Expr) bool {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Server" {
		return false
	}

	pkg, ok := selector.X.(*ast.Ident)

	return ok && pkg.Name == "http"
}
