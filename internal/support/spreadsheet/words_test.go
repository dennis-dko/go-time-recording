package spreadsheet

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/support/i18n"
)

// Every word a workbook writes is in every dictionary - a sheet's name, its
// headings, the values it translates - and no workbook.* entry is a word it no
// longer writes. A new language is one file, and a word left out of it is
// written in English in that language's workbook.
func TestEveryWordAWorkbookWritesIsInEveryDictionary(t *testing.T) {
	written := map[string]bool{}

	for _, table := range []Table{timesheets, projects, users, roles, statement} {
		written[table.Key] = true

		for _, heading := range table.Headings {
			written[heading] = true
		}
	}

	for _, word := range valueWords {
		written[word] = true
	}

	// And a word handed to translate as it is written, such as a statement's
	// total.
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}

		syntax, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}

		ast.Inspect(syntax, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 2 {
				return true
			}

			if ident, named := call.Fun.(*ast.Ident); !named || ident.Name != "translate" {
				return true
			}

			if literal, ok := call.Args[1].(*ast.BasicLit); ok && literal.Kind == token.STRING {
				word, _ := strconv.Unquote(literal.Value)
				written[word] = true
			}

			return true
		})
	}

	if !written["Total"] || len(written) < 25 {
		t.Fatalf("found %d words a workbook writes; this case is no longer reading them", len(written))
	}

	for _, language := range i18n.Languages()[1:] {
		words := i18n.Dictionary(language)

		for word := range written {
			if words["workbook."+word] == "" {
				t.Errorf("a workbook writes %q and %s has no workbook.%s", word, language, word)
			}
		}

		for key := range words {
			if english, ok := strings.CutPrefix(key, "workbook."); ok && !written[english] {
				t.Errorf("%s has %s, which no workbook writes", language, key)
			}
		}
	}
}
