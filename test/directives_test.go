package test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// CLAUDE.md points at exact lines, and nothing kept them pointing there.
//
// Seven places in that file name a `file.go:N` and say what is at it - the one
// permitted panic, the one SQL statement built with Sprintf, the one time.Sleep,
// the three deliberate calls to VisibleTo instead of RequireVisible. They are the
// difference between "audit this rule and read the exception" and "audit this
// rule and rediscover the exception", which is most of what makes an audit of
// this repository cheap rather than expensive.
//
// Two of the seven had drifted by the time anybody looked: the Sprintf exception
// had moved 1308 -> 1337 as the migration chain grew, and the VisibleTo call
// 259 -> 272. Neither is wrong in a way anything would report. An auditor
// following either reference reads an unremarkable line, concludes the exception
// is gone, and either files the real one as a violation or, worse, trusts the
// document over the tree.
//
// CLAUDE.md's own rule is that a directive which has stopped being true is worse
// than no directive. This is the mechanism that rule was missing.
//
// The expected substring is written here rather than parsed out of the prose
// around the reference. That is the duplication, and it is the point: this test
// is what compares the two, and it cannot do that by reading only one of them.
func TestCLAUDEmdStillPointsAtWhatItSaysItDoes(t *testing.T) {
	references := []struct {
		// path as CLAUDE.md writes it, which is sometimes only the base name.
		reference string
		// resolved is the path from the repository root.
		resolved string
		line     int
		contains string
	}{
		{
			reference: "internal/interface/web/web.go:85",
			resolved:  "internal/interface/web/web.go",
			line:      85,
			contains:  "panic(",
		},
		{
			reference: "internal/infrastructure/persistence/migrations/migrations.go:1337",
			resolved:  "internal/infrastructure/persistence/migrations/migrations.go",
			line:      1337,
			contains:  "fmt.Sprintf(",
		},
		{
			reference: "internal/infrastructure/config/datasource.go:113",
			resolved:  "internal/infrastructure/config/datasource.go",
			line:      113,
			contains:  "ConfigLocation",
		},
		{
			reference: "restart_handler.go:431",
			resolved:  "internal/interface/api/v1/rest/restart_handler.go",
			line:      431,
			contains:  "time.Sleep(",
		},
		{
			reference: "project_application_service.go:135",
			resolved:  "internal/application/v1/service/project_application_service.go",
			line:      135,
			contains:  "VisibleTo(",
		},
		{
			reference: "workbook_application_service.go:305",
			resolved:  "internal/application/v1/service/workbook_application_service.go",
			line:      305,
			contains:  "VisibleTo(",
		},
		{
			reference: "timesheet_application_service.go:272",
			resolved:  "internal/application/v1/service/timesheet_application_service.go",
			line:      272,
			contains:  "VisibleTo(",
		},
	}

	root := ".."
	directives := read(t, filepath.Join(root, "CLAUDE.md"))

	// Every reference in the table is one CLAUDE.md actually makes. Without this
	// the table could quietly outlive the sentence that needed it, and go on
	// passing about a reference nobody reads any more.
	for _, want := range references {
		if !strings.Contains(directives, "`"+want.reference+"`") {
			t.Errorf("this test expects CLAUDE.md to cite %s and it no longer does; "+
				"drop the row if the directive went, or fix the row if it moved",
				want.reference)
		}
	}

	// And every reference CLAUDE.md makes is one this table checks, or a new
	// directive would be added with nothing watching it.
	cited := regexp.MustCompile("`([a-zA-Z0-9_/.-]+\\.go):([0-9]+)`")

	for _, m := range cited.FindAllStringSubmatch(directives, -1) {
		found := false

		for _, want := range references {
			if want.reference == m[1]+":"+m[2] {
				found = true

				break
			}
		}

		if !found {
			t.Errorf("CLAUDE.md cites %s:%s and this test does not check it, "+
				"so nothing would notice when it drifts", m[1], m[2])
		}
	}

	for _, want := range references {
		lines := strings.Split(read(t, filepath.Join(root, want.resolved)), "\n")

		if want.line > len(lines) {
			t.Errorf("CLAUDE.md cites %s but the file has only %d lines",
				want.reference, len(lines))

			continue
		}

		if strings.Contains(lines[want.line-1], want.contains) {
			continue
		}

		// Say where it went. An auditor reading this failure wants the new line
		// number, not the news that the old one is wrong - and finding it by hand
		// in a 1,554-line migration chain is the tedious half of the job.
		t.Errorf("CLAUDE.md cites %s for %q and line %d reads %q%s",
			want.reference, want.contains, want.line,
			strings.TrimSpace(lines[want.line-1]), whereItWent(lines, want.contains))
	}
}

// whereItWent names the lines that do carry the token, so the fix is a number
// somebody copies rather than a search somebody repeats.
func whereItWent(lines []string, token string) string {
	var found []string

	for i, line := range lines {
		if strings.Contains(line, token) {
			found = append(found, strconv.Itoa(i+1))
		}
	}

	if len(found) == 0 {
		return "; it appears nowhere in the file, so the directive itself may be out of date"
	}

	return "; " + token + " is now at line " + strings.Join(found, ", ")
}

func read(t *testing.T, path string) string {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	return strings.ReplaceAll(string(content), "\r\n", "\n")
}
