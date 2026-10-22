package test

import (
	"path/filepath"
	"regexp"
	"slices"
	"testing"
)

// Everything erased with an account is handed over with it, and nothing else.
//
// Somebody asking for everything held about them (GDPR articles 15 and 20) is
// asking for exactly what PurgeUser would erase: the two are the same list read
// in two directions. A table added to the erasure and not to the export is data
// the person can lose without ever having been shown it; a table in the export
// and not in the erasure is data that outlives the account. Both lists are
// written out as statements, so they are compared as statements: the tables
// purge.go deletes from and the tables export.go reads from.
func TestEveryTableErasedWithAnAccountIsInItsExport(t *testing.T) {
	sqldb := filepath.Join("..", "internal", "infrastructure", "persistence", "sqldb")

	erased := tablesIn(read(t, filepath.Join(sqldb, "purge.go")), `DELETE FROM (\w+) WHERE`)
	exported := tablesIn(read(t, filepath.Join(sqldb, "export.go")), `FROM (\w+)(?: \w+)? (?:LEFT JOIN \w+ \w+ ON [\w.]+ = [\w.]+ )?WHERE`)

	if len(erased) < 10 {
		t.Fatalf("only %d tables were found in purge.go, so the scan has stopped reading it: %v", len(erased), erased)
	}

	for _, table := range erased {
		if !slices.Contains(exported, table) {
			t.Errorf("PurgeUser erases %s, and the export does not hand it over", table)
		}
	}

	for _, table := range exported {
		if !slices.Contains(erased, table) {
			t.Errorf("the export hands over %s, and PurgeUser does not erase it", table)
		}
	}
}

// tablesIn returns every table a pattern's first group names in a source, sorted
// and once each.
func tablesIn(source, pattern string) []string {
	var tables []string

	for _, match := range regexp.MustCompile(pattern).FindAllStringSubmatch(source, -1) {
		if !slices.Contains(tables, match[1]) {
			tables = append(tables, match[1])
		}
	}

	slices.Sort(tables)

	return tables
}
