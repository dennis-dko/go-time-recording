package migrations_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/migrations"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/sqldb"
)

// released is every version this chain had shipped when this test was written.
// It is a floor rather than a register: a version missing from it is fine as
// long as it comes after all of these.
var released = []int64{
	20260730120000, 20260730120100, 20260731010000, 20260731020000, 20260801010000,
	20260801010100, 20260801020000, 20260801030000, 20260801040000, 20260801050000,
	20260802010000, 20260803010000, 20260805010000, 20260805020000, 20260810010000,
	20260810020000, 20260810030000, 20260810040000, 20260810050000, 20260810060000,
	20260811010000, 20260812010000, 20260813010000, 20260814010000, 20260818010000,
	20260819010000, 20260824010000, 20260831010000,
}

// A migration added to the chain comes after every one before it.
//
// GoFr runs only the versions above the highest one an installation has already
// applied - migration.go compares each against MAX(version) and logs
// "skipping migration" for the rest, at INFO. So a migration added later with an
// older version is run by every fresh installation and skipped, quietly, by
// every one that already has the newer version: the two end with different
// schemas, and nothing says so. CLAUDE.md says to add to the end, and until this
// test that was a sentence. It is the shape parallel branches produce - each
// names its migration after the day it was started, and whichever merges second
// is the older one.
//
// Three checks, because each catches what the others cannot. The versions in
// the source are strictly increasing, which catches one appended with an older
// number. Every released version is still there, which is what append-only
// means. And every version this list has not seen is newer than all of it,
// which catches one slipped into the middle in its sorted place.
func TestEveryNewMigrationComesAfterEveryReleasedOne(t *testing.T) {
	inSource := versionsInSource(t)

	for i := 1; i < len(inSource); i++ {
		if inSource[i] <= inSource[i-1] {
			t.Errorf("migration %d follows %d in the chain; every installation that has "+
				"already run %d would skip it", inSource[i], inSource[i-1], inSource[i-1])
		}
	}

	chain := migrations.All(sqldb.DialectSQLite)

	for _, version := range released {
		if _, ok := chain[version]; !ok {
			t.Errorf("released migration %d is gone from the chain, which is append-only", version)
		}
	}

	newest := slices.Max(released)

	for version := range chain {
		if !slices.Contains(released, version) && version <= newest {
			t.Errorf("migration %d is not among the released ones and is not newer than %d, "+
				"so every installation that has run %d would skip it", version, newest, newest)
		}
	}

	if len(chain) != len(inSource) {
		t.Errorf("the chain has %d migrations and the source lists %d; this test is reading "+
			"the wrong literal", len(chain), len(inSource))
	}
}

// versionsInSource reads the keys of the map All returns, in the order they are
// written.
func versionsInSource(t *testing.T) []int64 {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "migrations.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing migrations.go: %v", err)
	}

	var versions []int64

	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "All" {
			return true
		}

		ast.Inspect(fn.Body, func(n ast.Node) bool {
			literal, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}

			if _, isMap := literal.Type.(*ast.MapType); !isMap {
				return true
			}

			for _, element := range literal.Elts {
				kv, ok := element.(*ast.KeyValueExpr)
				if !ok {
					continue
				}

				key, ok := kv.Key.(*ast.BasicLit)
				if !ok || key.Kind != token.INT {
					t.Fatalf("a key of the chain is not a literal version: %T", kv.Key)
				}

				version, err := strconv.ParseInt(key.Value, 10, 64)
				if err != nil {
					t.Fatalf("reading version %s: %v", key.Value, err)
				}

				versions = append(versions, version)
			}

			return false
		})

		return false
	})

	if len(versions) == 0 {
		t.Fatal("no versions were read from All; this test is reading nothing")
	}

	return versions
}
