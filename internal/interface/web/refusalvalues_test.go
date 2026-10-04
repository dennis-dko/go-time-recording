package web_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// A value a refusal carries is written the way the rest of the screen writes it.
//
// fillIn writes a number the way the tables do, and every string as it arrives.
// A day arrived in the order the wire uses and a status as the word it is stored
// under, so a German refusal said "am 2026-10-04" beside a form showing
// 04.10.2026, and "ist completed" beside a badge saying "abgeschlossen".
// REFUSAL_VALUES in app.js names, by code and position, which values are which;
// this reads every WithCode call the server makes, and fails on one sending a day
// or a status that the table does not name, and on a row nothing sends any more.
//
// The two kinds are recognised by how they are written, which is what the scan
// that found them saw: a day is a value formatted with time.DateOnly, a status is
// a field called Status or a model.ProjectStatus constant. A value spelled any
// other way is not seen, and that is the limit of what this can promise.
func TestARefusalWritesItsValuesTheWayTheScreenDoes(t *testing.T) {
	sent := refusalValueKinds(t)
	if len(sent) == 0 {
		t.Fatal("found no refusal that sends a day or a status; this case is reading nothing")
	}

	// The one refusal not made through WithCode: rest builds "not found" from
	// what was looked for, and its first value is the server's word for the kind
	// of record - "timesheet mit der Kennung 42 wurde nicht gefunden" on a German
	// screen until it went through entityName.
	if names := notFoundEntities(t); len(names) > 0 {
		sent["notFound#0"] = "entityName"

		dict := dictionaries(t)["de"]

		for _, name := range names {
			if _, found := dict["entity."+name]; !found {
				t.Errorf("the server can answer that a %s is not there, and the German "+
					"dictionary has no entity.%s to name it by", name, name)
			}
		}
	}

	block := regexp.MustCompile(`(?s)const REFUSAL_VALUES = \{(.*?)\n\};`).
		FindStringSubmatch(asset(t, "/app.js"))
	if block == nil {
		t.Fatal("app.js no longer declares REFUSAL_VALUES")
	}

	named := map[string]string{}

	for _, row := range regexp.MustCompile(`(\w+):\s*\{([^}]*)\}`).FindAllStringSubmatch(block[1], -1) {
		for _, entry := range regexp.MustCompile(`(\d+):\s*(\w+)`).FindAllStringSubmatch(row[2], -1) {
			named[row[1]+"#"+entry[1]] = entry[2]
		}
	}

	for at, writer := range sent {
		if named[at] != writer {
			t.Errorf("the server sends a value at %s that the screen writes with %s, and "+
				"REFUSAL_VALUES has %q there - the reader is shown it as it travels",
				at, writer, named[at])
		}
	}

	for at, writer := range named {
		if _, still := sent[at]; !still {
			t.Errorf("REFUSAL_VALUES writes %s with %s, and no refusal sends a day or a "+
				"status there any more", at, writer)
		}
	}

	// And every place a refusal's values go into a sentence asks the table. There
	// are two - a notice, and a row of an import preview - and they carry the same
	// refusals: a day over its ceiling is refused the same way one entry at a time
	// or eighty at once.
	js := asset(t, "/app.js")

	for _, direct := range regexp.MustCompile(`fillIn\([^;]*?\.(?:values|problemValues)\)`).
		FindAllString(js, -1) {
		if !strings.Contains(direct, "refusalValues(") {
			t.Errorf("a refusal's values go into its sentence past REFUSAL_VALUES: %s", direct)
		}
	}
}

// refusalValueKinds reads every WithCode call under internal/ and answers, for
// each value that is a day or a status, which function in app.js writes it:
// "code#position" -> "fmtDate" or "statusName".
//
// The same files serverErrorCodes reads, for the same reasons: not the tests,
// not apperror's own example, not the installer, which has a page of its own.
func refusalValueKinds(t *testing.T) map[string]string {
	t.Helper()

	root := filepath.Join("..", "..", "..", "internal")
	fset := token.NewFileSet()
	kinds := map[string]string{}

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") ||
			strings.HasSuffix(path, filepath.Join("apperror", "apperror.go")) ||
			strings.Contains(path, filepath.Join("interface", "installer")) {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}

		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) < 2 {
				return true
			}

			if method, ok := call.Fun.(*ast.SelectorExpr); !ok || method.Sel.Name != "WithCode" {
				return true
			}

			literal, ok := call.Args[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}

			code, err := strconv.Unquote(literal.Value)
			if err != nil {
				return true
			}

			for i, value := range call.Args[1:] {
				if writer := writtenWith(value); writer != "" {
					kinds[code+"#"+strconv.Itoa(i)] = writer
				}
			}

			return true
		})

		return nil
	})
	if err != nil {
		t.Fatalf("reading the server's refusals: %v", err)
	}

	return kinds
}

// notFoundEntities is every kind of record the server names in a "not found",
// read from its apperror.NotFound calls - and nothing when rest has stopped
// sending the kind as the refusal's first value, which this would then be
// guarding a value that no longer travels.
func notFoundEntities(t *testing.T) []string {
	t.Helper()

	root := filepath.Join("..", "..", "..", "internal")

	errorsGo, err := os.ReadFile(filepath.Join(root, "interface", "api", "v1", "rest", "errors.go"))
	if err != nil {
		t.Fatalf("reading rest's errors: %v", err)
	}

	if !strings.Contains(string(errorsGo), "values:  []any{detail.Entity, detail.ID}") {
		return nil
	}

	seen := map[string]bool{}
	call := regexp.MustCompile(`apperror\.NotFound\("([a-z]+)"`)

	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		for _, m := range call.FindAllSubmatch(source, -1) {
			seen[string(m[1])] = true
		}

		return nil
	})
	if err != nil {
		t.Fatalf("reading the server's not-found refusals: %v", err)
	}

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}

// writtenWith is the function in app.js that writes a value of this shape, or
// nothing for a value the screen shows as it arrives.
func writtenWith(value ast.Expr) string {
	switch v := value.(type) {
	case *ast.CallExpr:
		format, ok := v.Fun.(*ast.SelectorExpr)
		if !ok || format.Sel.Name != "Format" || len(v.Args) != 1 {
			return ""
		}

		if layout, ok := v.Args[0].(*ast.SelectorExpr); ok && layout.Sel.Name == "DateOnly" {
			if pkg, ok := layout.X.(*ast.Ident); ok && pkg.Name == "time" {
				return "fmtDate"
			}
		}
	case *ast.SelectorExpr:
		if v.Sel.Name == "Status" {
			return "statusName"
		}

		if pkg, ok := v.X.(*ast.Ident); ok && pkg.Name == "model" &&
			strings.HasPrefix(v.Sel.Name, "ProjectStatus") {
			return "statusName"
		}
	}

	return ""
}
