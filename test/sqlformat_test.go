package test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Every value formatted into SQL is named here, with why nothing but a constant
// can reach it.
//
// A statement built with Sprintf is one the driver cannot parameterise, so each
// is a place where the only protection is that nobody passes it anything else.
// There are three, and all three are safe. CLAUDE.md called the first "the one
// documented exception" for as long as the other two had existed: a count that
// carried a rule, with nothing watching it, and found wrong by a read rather
// than by a check. A line-at-a-time grep misses one of them, because its format
// string runs over two lines - which is why this parses the files instead.
//
// Keyed by the function rather than the line, so the row survives the file
// growing around it; a row whose function no longer formats SQL fails too.
func TestEveryValueFormattedIntoSQLIsNamed(t *testing.T) {
	named := map[string]string{
		"cmd/main.go:makeSQLiteWait": "sqliteBusyTimeout, a constant; " +
			"a PRAGMA takes no parameters, so there is no other way to write it",

		"internal/infrastructure/persistence/migrations/migrations.go:addRoleBasedAccess": "model.RoleUser, " +
			"a constant, and the constant rather than a literal on purpose - the comment above it says why",

		"internal/infrastructure/persistence/migrations/migrations.go:addSessionsAndPreferences": "the " +
			"literal reports:read and model.RoleAdmin, a constant; the migration has run everywhere and stays as written",
	}

	found := map[string][]int{}

	for _, root := range []string{"internal", "cmd"} {
		walk(t, filepath.Join("..", root), func(path, body string) {
			shown := filepath.ToSlash(strings.TrimPrefix(filepath.ToSlash(path), "../"))

			for function, lines := range sqlFormattedIn(t, path, body) {
				found[shown+":"+function] = append(found[shown+":"+function], lines...)
			}
		})
	}

	if len(found) == 0 {
		t.Fatal("no SQL formatted with a value found at all; this test is reading nothing")
	}

	for _, site := range sortedKeys(found) {
		if _, known := named[site]; !known {
			t.Errorf("%s formats a value into SQL at line(s) %v and is not named here. "+
				"Pass the value as a parameter instead; if the statement cannot take one "+
				"(a PRAGMA, an identifier), name it here with why nothing but a constant "+
				"can reach it", site, found[site])
		}
	}

	for _, site := range sortedKeys(named) {
		if _, still := found[site]; !still {
			t.Errorf("%s is named here and no longer formats SQL; drop the row, "+
				"or the list stops describing the tree", site)
		}
	}
}

// sqlStatement is what makes a format string SQL rather than a sentence: a
// statement that reads or writes rows, or a PRAGMA. Table definitions are
// formatted with column types throughout the migrations, and a type is not a
// value, so CREATE and ALTER are not on it.
var sqlStatement = regexp.MustCompile(`(?i)\b(?:SELECT|INSERT|UPDATE|DELETE|PRAGMA)\b`)

// sqlFormattedIn returns, per enclosing function, the lines where fmt.Sprintf is
// handed a constant format string that is SQL and carries a verb.
func sqlFormattedIn(t *testing.T, path, body string) map[string][]int {
	t.Helper()

	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, path, body, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	out := map[string][]int{}

	for _, decl := range file.Decls {
		function, ok := decl.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}

		ast.Inspect(function.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !isSprintf(call) || len(call.Args) == 0 {
				return true
			}

			format, constant := literalString(call.Args[0])
			if constant && sqlStatement.MatchString(format) && strings.Contains(
				strings.ReplaceAll(format, "%%", ""), "%") {
				out[function.Name.Name] = append(out[function.Name.Name],
					fset.Position(call.Pos()).Line)
			}

			return true
		})
	}

	return out
}

func isSprintf(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Sprintf" {
		return false
	}

	pkg, ok := selector.X.(*ast.Ident)

	return ok && pkg.Name == "fmt"
}

// literalString folds a string literal, or literals joined with +, into its
// value; anything else is not a constant this can read.
func literalString(expr ast.Expr) (string, bool) {
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

		left, ok := literalString(e.X)
		if !ok {
			return "", false
		}

		right, ok := literalString(e.Y)

		return left + right, ok
	case *ast.ParenExpr:
		return literalString(e.X)
	}

	return "", false
}
