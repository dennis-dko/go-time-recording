//go:build integration

package integration

import (
	"os"
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/test/harness"
)

// Only PostgreSQL refuses an entry that points at an account that is not there.
//
// Every foreign key in the migration chain is declared on its column - user_id
// INT NOT NULL REFERENCES users(id) - and the engines disagree about what that
// means. PostgreSQL makes a key of it and enforces it. MySQL parses it and makes
// nothing: measured on 8.4.11, information_schema lists no key for any table the
// chain created, and the row below is stored, because only the table-level
// FOREIGN KEY clause creates one there. SQLite keeps the declaration but enforces
// it only on a connection that asks, and whether the application's does is not
// something this suite's own connection can answer, so that leg is skipped.
//
// What rests on this is prose. Several comments, and CLAUDE.md, reason about
// what happens when a table is left out of the account purge or a project is
// removed from under a running clock, and each of them names the engines that
// refuse. They were written saying PostgreSQL and MySQL; they say PostgreSQL
// now, and this case is what they are written against. It fails the moment an
// engine starts or stops refusing, which is the moment they stop being true.
func TestOnlyPostgreSQLRefusesAnEntryPointingAtNobody(t *testing.T) {
	t.Parallel()

	dsn := os.Getenv(harness.DSNEnv)
	if dsn == "" {
		t.Skip("SQLite enforces a foreign key per connection, and this suite's " +
			"connection cannot speak for the application's")
	}

	a := start(t)

	db := a.DB(t)
	if db == nil {
		t.Fatal("this instance has no database")
	}

	postgres := strings.Contains(dsn, "postgres")

	insert := "INSERT INTO timesheets (user_id, date, duration_hours) " +
		"VALUES (?, '2026-07-03 00:00:00', 1)"
	if postgres {
		insert = strings.Replace(insert, "?", "$1", 1)
	}

	_, err := db.Exec(insert, 987654321)

	switch {
	case postgres && err == nil:
		t.Error("PostgreSQL stored an entry for an account that does not exist; " +
			"the comments saying it refuses one are wrong now")
	case !postgres && err != nil:
		t.Errorf("this engine refused an entry for an account that does not exist (%v); "+
			"the comments saying only PostgreSQL does are wrong now", err)
	}
}
