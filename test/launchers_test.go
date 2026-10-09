package test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every browser the browser suite starts gets its options from launch.
//
// launch holds what is not about any one case: no sandbox where CI cannot have
// one, the browser CHROME_PATH names, how long a start may take, and that no
// name resolves but localhost. Its own comment says a fourth list written
// beside it would have had to remember all of that, and one had been written:
// the installer's prefill case built its options from chromedp's defaults
// itself, so it had neither the start timeout nor the resolver rule when the
// second arrived. A rule that had to be remembered had already been forgotten
// once, so a list built from those defaults anywhere but in launch is refused.
func TestEveryBrowserTheSuiteStartsComesFromLaunch(t *testing.T) {
	dir := filepath.Join("..", "test", "browser")

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read the browser suite: %v", err)
	}

	fset := token.NewFileSet()
	inLaunch := 0

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}

		file, err := parser.ParseFile(fset, filepath.Join(dir, entry.Name()), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", entry.Name(), err)
		}

		for _, decl := range file.Decls {
			fn, isFunc := decl.(*ast.FuncDecl)

			ast.Inspect(decl, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "DefaultExecAllocatorOptions" {
					return true
				}

				if isFunc && fn.Recv == nil && fn.Name.Name == "launch" {
					inLaunch++

					return true
				}

				t.Errorf("%s builds a browser's options from chromedp's defaults itself; start it with launch instead",
					fset.Position(sel.Pos()))

				return true
			})
		}
	}

	if inLaunch == 0 {
		t.Error("launch no longer builds on chromedp's defaults, so this check no longer knows where a browser's options come from")
	}
}
