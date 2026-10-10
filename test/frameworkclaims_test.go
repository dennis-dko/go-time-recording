package test

import (
	"os"
	"regexp"
	"testing"
)

// frameworkClaimsReadAgainst is the GoFr version the comments saying what the
// framework cannot do were last read against, sentence by sentence.
//
// A workaround here carries its reason in a comment, and the reason is a fact
// about GoFr at the version it was written for. When the requirement moves, the
// fact can stop being true while the sentence stays - and a reason that has
// stopped being true is worse than none, because it is the one sentence between
// a reader and the obvious simplification. Measured once already: the log level
// was applied by the sink "because GoFr's ChangeLevel is a data race" for three
// weeks after v1.60.0 had made that field atomic.
//
// Moved only by whoever has run both halves of the sweep doc/audit-method.md
// describes against the new version - the scan of every sentence in internal/
// and cmd/ naming GoFr or the framework beside a word of limitation, and the
// read of GoFr's own diff between the two versions - and corrected what no
// longer holds. The scan alone is not the sweep: at v1.62.0 the diff found all
// five claims that had stopped being true, and the scan none of them.
const frameworkClaimsReadAgainst = "v1.62.0"

// TestTheFrameworkClaimsWereReadAgainstTheGoFrRequired fails once go.mod requires
// another GoFr than the one the claims were last read against.
func TestTheFrameworkClaimsWereReadAgainstTheGoFrRequired(t *testing.T) {
	raw, err := os.ReadFile("../go.mod")
	if err != nil {
		t.Fatal(err)
	}

	required := regexp.MustCompile(`(?m)^\s*gofr\.dev (v\S+)`).FindSubmatch(raw)
	if required == nil {
		t.Fatal("go.mod requires no gofr.dev, or this no longer reads it")
	}

	if got := string(required[1]); got != frameworkClaimsReadAgainst {
		t.Errorf("go.mod requires GoFr %s, and what the comments say it cannot do was last read "+
			"against %s: run the sweep in doc/audit-method.md, correct what no longer holds, then move "+
			"frameworkClaimsReadAgainst", got, frameworkClaimsReadAgainst)
	}
}
