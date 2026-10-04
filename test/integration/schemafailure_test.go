//go:build integration

package integration

import (
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/test/harness"
)

// A migration that fails says so where whoever runs the installation will look.
//
// GoFr stops the process when one fails - it rolls it back and calls Fatal - and
// a Fatal goes through the pipe the log viewer reads, so the reason can be gone
// before anything passed it on: measured, one of the first two runs of this case
// exited 1 with an empty log. That is the one message an installation whose
// upgrade did not apply needs, and the migration is the application's own, so
// the application says why itself, past the pipe, before GoFr ends the process.
// A database already holding a table of the same name - another application's,
// a dump restored into the wrong place - is the plainest way to meet it.
func TestAMigrationThatFailsSaysWhyBeforeTheProcessEnds(t *testing.T) {
	t.Parallel()

	if os.Getenv(harness.DSNEnv) != "" {
		t.Skip("prepares a SQLite file; the server dialects meet the same refusal through the same code")
	}

	shared := harness.SharedDatabase(t)

	db, err := sql.Open("sqlite", "file:"+shared+".db")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY, nickname TEXT)"); err != nil {
		t.Fatalf("preparing somebody else's table: %v", err)
	}

	_ = db.Close()

	code, log := harness.StartExpectingExit(t, "DB_NAME="+shared)

	if code == 0 {
		t.Errorf("a start whose first migration failed exited 0")
	}

	if !strings.Contains(log, "migration 20260730120000 could not be applied") {
		t.Errorf("the start ended without saying which migration could not be applied:\n%s",
			truncate(log, 2000))
	}
}
