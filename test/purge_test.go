package test

import (
	"path/filepath"
	"regexp"
	"sort"
	"testing"
)

// Every table that points at an account is emptied when the account is erased.
//
// PurgeUser is the erasure path. Recorded hours are what somebody is paid for,
// so the account and its record go together, and directory synchronisation calls
// this as well as an administrator deleting somebody deliberately. It is
// irreversible either way.
//
// A table missing from its list does not fail loudly, which is the whole reason
// for this. On PostgreSQL and MySQL the final DELETE is refused while anything
// still references the account - and by then the time entries are already gone,
// inside a transaction that now rolls back, so the operation simply never
// succeeds. On SQLite, where foreign keys are not enforced unless asked for, it
// succeeds and leaves rows pointing at an account that no longer exists.
//
// This has already happened once, with passkeys. CLAUDE.md records it, and
// records the remedy as "add it to the list in the same commit" - which is a rule
// held up by whoever remembers it at the time.
//
// It also records why the obvious scan is not enough: `grep 'REFERENCES
// users(id)'` misses projects.owner_id, which was added by an ALTER and appears
// in no CREATE TABLE. So this reads the whole chain for both shapes, and follows
// RENAME TO - the SQLite rebuild of timesheets creates timesheets_new and renames
// it, and a check that did not follow that would be looking for a table that does
// not exist under a name nothing uses.
func TestEveryTableReferencingAnAccountIsPurgedWithIt(t *testing.T) {
	root := ".."
	chain := read(t, filepath.Join(root,
		"internal", "infrastructure", "persistence", "migrations", "migrations.go"))
	purge := read(t, filepath.Join(root,
		"internal", "infrastructure", "persistence", "sqldb", "purge.go"))

	referencing := referencesToUsers(chain)
	if len(referencing) == 0 {
		t.Fatal("no references to users(id) found; this test is reading nothing")
	}

	// The account's own table is not one of the things deleted before it.
	delete(referencing, "users")

	var missing []string

	for table, column := range referencing {
		deletion := regexp.MustCompile(
			`DELETE FROM ` + regexp.QuoteMeta(table) + `\s+WHERE\s+` +
				regexp.QuoteMeta(column) + `\s*=`)

		if !deletion.MatchString(purge) {
			missing = append(missing, table+"."+column)
		}
	}

	sort.Strings(missing)

	for _, one := range missing {
		t.Errorf("%s points at an account and PurgeUser does not clear it; on "+
			"PostgreSQL and MySQL that makes the erasure impossible after the hours "+
			"are already deleted, and on SQLite it leaves rows behind pointing at "+
			"nobody", one)
	}
}

// referencesToUsers maps each table with a foreign key to users onto the column
// that holds it, with renames applied.
func referencesToUsers(chain string) map[string]string {
	found := map[string]string{}

	// Inside a CREATE TABLE body: the column line names the column, and the
	// nearest CREATE TABLE above it names the table.
	created := regexp.MustCompile(`CREATE TABLE (?:IF NOT EXISTS )?([a-z_]+)`)
	inTable := regexp.MustCompile(`(?m)^\s*([a-z_]+)\s+\S+.*REFERENCES users\(id\)`)

	for _, at := range inTable.FindAllStringSubmatchIndex(chain, -1) {
		column := chain[at[2]:at[3]]

		tables := created.FindAllStringSubmatchIndex(chain[:at[0]], -1)
		if len(tables) == 0 {
			continue
		}

		last := tables[len(tables)-1]
		found[chain[last[2]:last[3]]] = column
	}

	// And added later by an ALTER, which is how projects.owner_id arrived and why
	// reading only the CREATE statements is not enough.
	altered := regexp.MustCompile(
		`ALTER TABLE ([a-z_]+) ADD COLUMN ([a-z_]+)[^"` + "`" + `]*REFERENCES users\(id\)`)

	for _, m := range altered.FindAllStringSubmatch(chain, -1) {
		found[m[1]] = m[2]
	}

	return withRenames(found, chain)
}

// withRenames follows ALTER TABLE ... RENAME TO, so a table is looked for under
// the name it ends up with.
func withRenames(found map[string]string, chain string) map[string]string {
	renamed := regexp.MustCompile(`ALTER TABLE ([a-z_]+) RENAME TO ([a-z_]+)`)

	for _, m := range renamed.FindAllStringSubmatch(chain, -1) {
		if column, ok := found[m[1]]; ok {
			delete(found, m[1])

			found[m[2]] = column
		}
	}

	return found
}
