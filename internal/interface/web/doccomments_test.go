package web_test

import (
	"strings"
	"testing"
)

// A doc comment in the scripts belongs to the declaration directly below it.
//
// Eleven of them in app.js sat above another declaration's doc comment instead:
// somebody had put a new declaration, with its own comment, between a comment
// and the function it described. The function then stood further down with no
// description at all, and an editor showed both comments over the newcomer.
// Reading the file does not show it - each comment still reads correctly where
// it is - and test/doccomments_test.go, which asks the same of the Go side,
// never looks at the scripts.
//
// So: a line opening a doc comment may not follow directly on a line that
// closes one.
func TestNoDocCommentInTheScriptsIsCutLooseFromItsDeclaration(t *testing.T) {
	for _, script := range []string{"/app.js", "/api-docs.js", "/theme.js"} {
		lines := strings.Split(asset(t, script), "\n")

		for i := 1; i < len(lines); i++ {
			if !strings.HasPrefix(strings.TrimSpace(lines[i]), "/**") {
				continue
			}

			if !strings.HasSuffix(strings.TrimSpace(lines[i-1]), "*/") {
				continue
			}

			t.Errorf("%s:%d: a doc comment follows directly on another, so the one above "+
				"describes nothing below it: %.80s", script, i+1, strings.TrimSpace(lines[i-1]))
		}
	}
}
