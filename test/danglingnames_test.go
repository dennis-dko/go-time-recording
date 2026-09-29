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

// A comment that names something names something that exists.
//
// A comment pointing at a function is how a reader is sent to the reason for a
// line, and when the function is renamed the pointer goes on pointing at
// nothing. Five of those were found at once, none by any check: "see
// checkDailyLimit" over the constant whose history lives in checkDailyBudget,
// "see toIcon" twice for what imaging.ToIcon does, error codes quoted as
// examples that had been retired or renamed, and a test's comment describing a
// handler that is no longer declared under it. Each reads correctly on its own;
// the reader who follows it finds nothing and concludes the reason is gone.
//
// The words asked about are camelCase ones - a lowercase start and an inner
// capital - because those are identifiers rather than prose. A word no Go file
// and no part of app.js declares or uses fails, unless it is named below.
func TestEveryNameACommentPointsAtExists(t *testing.T) {
	names, mentions := commentMentions(t)

	if len(names) < 5000 {
		t.Fatalf("only %d identifiers were found; the walk has stopped reading the tree", len(names))
	}

	for _, m := range mentions {
		if names[m.word] {
			continue
		}

		if _, known := namedOnPurpose[m.word]; known {
			continue
		}

		t.Errorf("%s: a comment names %s, which nothing in the tree is called. Point it at "+
			"what the thing is called now, or, if it names something outside this "+
			"repository or something that is gone on purpose, give it a row in namedOnPurpose",
			m.at, m.word)
	}
}

// TestNothingIsNamedOnPurposeThatExistsOrIsUnmentioned keeps namedOnPurpose
// from outliving the comments it excuses.
func TestNothingIsNamedOnPurposeThatExistsOrIsUnmentioned(t *testing.T) {
	names, mentions := commentMentions(t)

	mentioned := map[string]bool{}
	for _, m := range mentions {
		mentioned[m.word] = true
	}

	for _, word := range sortedKeys(namedOnPurpose) {
		switch {
		case names[word]:
			t.Errorf("%s is named on purpose as something the tree does not declare, and "+
				"the tree now declares it; drop the row", word)
		case !mentioned[word]:
			t.Errorf("%s is named on purpose and no comment mentions it any more; drop the row", word)
		}
	}
}

// namedOnPurpose is what comments may name without the tree declaring it.
var namedOnPurpose = map[string]string{
	// Somebody else's names.
	"initMetricsServer": "GoFr's, which metricsPort mirrors",
	"toLocale":          "the prefix of the JavaScript toLocale* methods",
	"checkVisibility":   "a DOM method",
	"getValueFrom":      "excelize's, where the spilled shared string is read",
	"getFromStringItem": "excelize's, where the spilled shared string is read",
	"sharedStringItem":  "excelize's",
	"offsetRange":       "excelize's",

	// Gone on purpose, and the comment says so where it stood.
	"mayShare":                  "retired with the shared project",
	"requireMayWrite":           "retired with the shared project",
	"requireMayDelete":          "retired with the shared project",
	"parseYesNo":                "retired with the Category column",
	"signInAsAuditor":           "retired with timesheets:read:all",
	"defaultAutoCloseAfterDays": "the constant orphans_test.go was written after",

	// An example of the shape a check looks for.
	"requireAdministrator": "what doccomments_test.go means by a compound identifier",

	// The history this check was written after, told above it.
	"checkDailyLimit": "the name the daily cap's comment pointed at",
	"toIcon":          "the name two comments gave imaging.ToIcon",
}

type commentMention struct {
	at   string
	word string
}

var camelCase = regexp.MustCompile(`\b[a-z]+(?:[A-Z][a-z0-9]+)+\b`)

// commentMentions returns every identifier in the tree, with the words app.js
// uses, and every camelCase word a Go comment carries.
func commentMentions(t *testing.T) (map[string]bool, []commentMention) {
	t.Helper()

	names := map[string]bool{}

	var mentions []commentMention

	fset := token.NewFileSet()

	for _, root := range []string{"internal", "cmd", "test", "build"} {
		for _, path := range goFilesAndTestsUnder(t, filepath.Join("..", root)) {
			file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if err != nil {
				t.Fatalf("parsing %s: %v", path, err)
			}

			ast.Inspect(file, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok {
					names[id.Name] = true
				}

				return true
			})

			shown := filepath.ToSlash(strings.TrimPrefix(filepath.ToSlash(path), "../"))

			for _, group := range file.Comments {
				for _, c := range group.List {
					for _, word := range camelCase.FindAllString(c.Text, -1) {
						mentions = append(mentions, commentMention{
							at:   shown + ":" + strconv.Itoa(fset.Position(c.Pos()).Line),
							word: word,
						})
					}
				}
			}
		}
	}

	// Go comments point at the interface's functions too, which live in app.js.
	js, err := os.ReadFile(filepath.Join("..", "internal", "interface", "web", "assets", "app.js"))
	if err != nil {
		t.Fatalf("reading app.js: %v", err)
	}

	for _, word := range regexp.MustCompile(`\b[A-Za-z_]\w*\b`).FindAllString(string(js), -1) {
		names[word] = true
	}

	return names, mentions
}
