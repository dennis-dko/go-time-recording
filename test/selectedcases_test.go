package test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// A case that needs a real directory or a real collector runs in one place
// only, and that place chooses its cases by name.
//
// Such a case guards itself on an environment variable and skips wherever the
// variable is not set, which is every job but one. That one - "Directory and
// tracing tests" in ci.yml, and `task test:ldap` and `task test:traces` here -
// runs `go test -run '<words>'`. So a case whose name carries none of the words
// is skipped where it could have run and not selected where it would have: it
// runs nowhere, and every job is green. The workflow fails on a case that
// skips inside that job, which is how the first such gap was closed; it cannot
// see a case the pattern never picked.
//
// Two cases written in the commit this file arrived with were named that way
// first, and passed here only because they were run by name.
//
// The guard is found by what a case calls rather than by a list, so the next
// one is held without being added anywhere.
var needsAService = map[string]struct {
	what string

	// task is the Taskfile target that runs these cases locally.
	task string
}{
	"requireLDAP":   {"a real directory", "test:ldap"},
	"requireJaeger": {"a real collector", "test:traces"},
}

// selection matches the pattern a `go test` line chooses its cases with,
// quoted or bare.
var selection = regexp.MustCompile(`-run '?([A-Za-z|.*^$()\[\]]+)'?`)

// patternAfter is the first -run pattern after a marker in a file: the step or
// the task that marker names.
func patternAfter(t *testing.T, file, marker string) *regexp.Regexp {
	t.Helper()

	text := read(t, filepath.Join("..", filepath.FromSlash(file)))

	at := strings.Index(text, marker)
	if at < 0 {
		t.Fatalf("%s no longer carries %q, which is where this looks for the cases it selects", file, marker)
	}

	found := selection.FindStringSubmatch(text[at:])
	if found == nil {
		t.Fatalf("%s selects no cases by name after %q", file, marker)
	}

	pattern, err := regexp.Compile(found[1])
	if err != nil {
		t.Fatalf("%s selects cases with %q, which is not a pattern: %v", file, found[1], err)
	}

	return pattern
}

func TestEveryCaseThatNeedsAServiceIsOneTheJobForItSelects(t *testing.T) {
	inCI := patternAfter(t, ".github/workflows/ci.yml", "name: Directory and tracing tests")

	byTask := map[string]*regexp.Regexp{}
	for _, need := range needsAService {
		byTask[need.task] = patternAfter(t, "Taskfile.yml", "\n  "+need.task+":")
	}

	files, err := filepath.Glob(filepath.Join("integration", "*_test.go"))
	if err != nil {
		t.Fatal(err)
	}

	sort.Strings(files)

	fset := token.NewFileSet()
	held := 0

	for _, file := range files {
		parsed, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing %s: %v", file, err)
		}

		for _, declared := range parsed.Decls {
			fn, ok := declared.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Body == nil || !strings.HasPrefix(fn.Name.Name, "Test") {
				continue
			}

			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}

				guard, ok := call.Fun.(*ast.Ident)
				if !ok {
					return true
				}

				need, ok := needsAService[guard.Name]
				if !ok {
					return true
				}

				held++

				name := fn.Name.Name

				if !inCI.MatchString(name) {
					t.Errorf("%s needs %s and the job that has one selects cases with %q, which does not "+
						"match it - so it skips in every other job and is not run in that one",
						name, need.what, inCI)
				}

				if local := byTask[need.task]; !local.MatchString(name) {
					t.Errorf("%s needs %s and `task %s` selects cases with %q, which does not match it",
						name, need.what, need.task, local)
				}

				return true
			})
		}
	}

	// A sweep that found nothing to hold is a sweep that has stopped looking.
	if held < 10 {
		t.Errorf("only %d case(s) were found asking for a directory or a collector; "+
			"the guards may have been renamed", held)
	}
}
