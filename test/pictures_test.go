package test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The README shows the pictures the camera takes, and the camera takes the
// pictures the README shows.
//
// The camera is a case in the browser suite that runs only when asked, so
// nothing else notices when the two part. A picture the README names and
// doc/image does not hold is a broken image on the page most people see first;
// one doc/image holds and nothing shows is downloaded by every clone and never
// looked at; and one the camera does not take is never taken again, so it shows
// a screen as it was the day somebody put it there by hand.
func TestTheReadmeShowsWhatTheCameraTakes(t *testing.T) {
	t.Parallel()

	taken := picturesTheCameraTakes(t)
	shown := picturesTheDocumentationShows(t)

	kept := map[string]bool{}

	entries, err := os.ReadDir(filepath.Join("..", "doc", "image"))
	if err != nil {
		t.Fatalf("reading doc/image: %v", err)
	}

	for _, entry := range entries {
		kept[entry.Name()] = true
	}

	if len(taken) < 10 || len(shown) < 10 {
		t.Fatalf("the camera takes %d pictures and the documentation shows %d; the "+
			"reading has stopped finding them", len(taken), len(shown))
	}

	for _, name := range sortedKeys(shown) {
		if !kept[name] {
			t.Errorf("%s shows doc/image/%s, which is not there", shown[name], name)
		}

		if !taken[name] {
			t.Errorf("%s shows doc/image/%s, which the camera does not take - so it "+
				"is never taken again, and shows the screen as it was", shown[name], name)
		}
	}

	for _, name := range sortedKeys(kept) {
		if _, ok := shown[name]; !ok {
			t.Errorf("doc/image/%s is shown nowhere; drop it, or show it", name)
		}
	}

	for _, name := range sortedKeys(taken) {
		if !kept[name] {
			t.Errorf("the camera takes %s and doc/image holds none; run task readme:pictures", name)
		}
	}
}

// picturesTheCameraTakes reads the file names the camera's case writes: the
// second argument of shoot and shootCard, and the third of keep.
func picturesTheCameraTakes(t *testing.T) map[string]bool {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(),
		filepath.Join("..", "test", "browser", "readmepictures_test.go"), nil, 0)
	if err != nil {
		t.Fatalf("parsing the camera: %v", err)
	}

	position := map[string]int{"shoot": 1, "shootCard": 1, "keep": 2}
	taken := map[string]bool{}

	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		var name string

		switch fun := call.Fun.(type) {
		case *ast.Ident:
			name = fun.Name
		case *ast.SelectorExpr:
			name = fun.Sel.Name
		}

		at, ok := position[name]
		if !ok || len(call.Args) <= at {
			return true
		}

		if literal, ok := call.Args[at].(*ast.BasicLit); ok && literal.Kind == token.STRING {
			if picture, err := strconv.Unquote(literal.Value); err == nil {
				taken[picture+".png"] = true
			}
		}

		return true
	})

	return taken
}

// picturesTheDocumentationShows answers each picture in doc/image that the
// README or a page beside it shows, with the first document that shows it.
func picturesTheDocumentationShows(t *testing.T) map[string]string {
	t.Helper()

	documents := []string{"README.md"}

	pages, err := filepath.Glob(filepath.Join("..", "doc", "*.md"))
	if err != nil {
		t.Fatalf("listing doc: %v", err)
	}

	for _, page := range pages {
		documents = append(documents, filepath.ToSlash(strings.TrimPrefix(page, ".."+string(filepath.Separator))))
	}

	picture := regexp.MustCompile(`image/([a-z0-9-]+\.png)`)
	shown := map[string]string{}

	for _, document := range documents {
		text, err := os.ReadFile(filepath.Join("..", document))
		if err != nil {
			t.Fatalf("reading %s: %v", document, err)
		}

		for _, match := range picture.FindAllStringSubmatch(string(text), -1) {
			if _, ok := shown[match[1]]; !ok {
				shown[match[1]] = document
			}
		}
	}

	return shown
}
