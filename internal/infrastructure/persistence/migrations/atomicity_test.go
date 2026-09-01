package migrations

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"slices"
	"sort"
	"testing"
)

// A migration that changes the schema and the data is not atomic on MySQL.
//
// GoFr runs each migration inside a transaction - migration.Datasource.SQL *is*
// the *sql.Tx - so every statement in one, including the row-at-a-time work in
// giveEveryProjectAnOwner, commits or rolls back together. On PostgreSQL and
// SQLite that is the end of it.
//
// MySQL commits implicitly on DDL. A migration that alters a table and then
// updates rows therefore runs its UPDATE outside the transaction the migration
// began: the schema change has already landed, and if the data change fails,
// nothing goes back. The migration is recorded as failed with half of it on disk.
//
// What that costs is not abstract. rememberWhenASessionWasLastUsed adds
// last_seen_at and then fills it from created_at, and its own comment says why:
// leaving existing sessions at the zero time "would sign out everybody who is
// signed in the second an installation updates". Split on MySQL, that is exactly
// what happens - the migration written to prevent it becomes the cause of it.
//
// Six existing migrations mix the two. They are named here rather than fixed,
// because the chain is append-only: they have run on every installation that
// exists, and editing an applied migration is the one thing this file must never
// do. This is a guard for the seventh.
//
// A new migration that needs both is two migrations - the schema in one, the data
// in the next - which is atomic on every engine, and is CLAUDE.md §7's
// multi-phase rule applied to the engine rather than to the deployment.
func TestNoNewMigrationMixesSchemaAndDataChanges(t *testing.T) {
	// Applied long ago, on installations that exist. Fixed history, not a backlog.
	alreadyMixed := []string{
		"addPrivateProjects",
		"addRoleBasedAccess",
		"addSessionsAndPreferences",
		"makeProjectOptional",
		"rememberWhenASessionWasLastUsed",
		"retireTheReviewPath",
	}

	chain := readChain(t)

	entries := chain.calls["All"]
	if len(entries) == 0 {
		t.Fatal("no migrations found through All; this test is reading nothing")
	}

	var mixed []string

	for _, name := range entries {
		text := chain.reach(name)

		if changesSchema.MatchString(text) && changesData.MatchString(text) {
			mixed = append(mixed, name)
		}
	}

	sort.Strings(mixed)

	for _, name := range mixed {
		if !slices.Contains(alreadyMixed, name) {
			t.Errorf("the migration %s changes the schema and the data, which MySQL "+
				"cannot roll back as a unit: its DDL commits implicitly, so the data "+
				"change after it runs outside the migration's transaction and a "+
				"failure leaves half of it applied. Split it into two migrations, "+
				"schema first", name)
		}
	}

	for _, name := range alreadyMixed {
		if !slices.Contains(mixed, name) {
			t.Errorf("%s is listed here as already mixing the two and no longer "+
				"does; if it was split, take it off the list - and if it was edited "+
				"in place, that is the one thing an applied migration must never be",
				name)
		}
	}
}

var (
	changesSchema = regexp.MustCompile(`\b(CREATE TABLE|ALTER TABLE|DROP TABLE|CREATE INDEX|DROP INDEX)\b`)
	changesData   = regexp.MustCompile(`\b(INSERT INTO|UPDATE [a-z_]+ SET|DELETE FROM)\b`)
)

// chain is the migration file: what each function says, and what it calls.
type chain struct {
	source map[string]string
	calls  map[string][]string
}

// reach is a function's own source plus that of everything it calls, so a
// migration whose statements live in a helper is still seen whole.
//
// Calls come from the syntax tree rather than from a text search, or a name
// mentioned in a comment or inside a SQL string would count as a call - and All,
// which names every migration there is, would then reach all of them and report
// the whole chain as one mixed migration.
func (c chain) reach(name string) string {
	seen := map[string]bool{}

	var walk func(string) string

	walk = func(at string) string {
		if seen[at] || c.source[at] == "" {
			return ""
		}

		seen[at] = true
		out := c.source[at]

		for _, next := range c.calls[at] {
			out += "\n" + walk(next)
		}

		return out
	}

	return walk(name)
}

func readChain(t *testing.T) chain {
	t.Helper()

	const path = "migrations.go"

	fset := token.NewFileSet()

	parsed, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	out := chain{source: map[string]string{}, calls: map[string][]string{}}

	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Body == nil {
			continue
		}

		from := fset.Position(fn.Pos()).Offset
		to := fset.Position(fn.End()).Offset
		out.source[fn.Name.Name] = string(body[from:to])

		seen := map[string]bool{}

		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, isCall := node.(*ast.CallExpr)
			if !isCall {
				return true
			}

			if named, isNamed := call.Fun.(*ast.Ident); isNamed && !seen[named.Name] {
				seen[named.Name] = true

				out.calls[fn.Name.Name] = append(out.calls[fn.Name.Name], named.Name)
			}

			return true
		})
	}

	// Nothing reaches back into All, or every migration would appear to contain
	// every other one.
	delete(out.source, "All")

	return out
}
