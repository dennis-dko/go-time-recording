package sqldb

import (
	"testing"
	"time"
)

func TestRebindRewritesPlaceholdersForPostgresOnly(t *testing.T) {
	const query = "SELECT id FROM users WHERE name = ? AND role = ? AND id > ?"

	cases := map[string]struct {
		dialect string
		want    string
	}{
		"postgres": {DialectPostgres, "SELECT id FROM users WHERE name = $1 AND role = $2 AND id > $3"},
		"sqlite":   {DialectSQLite, query},
		"mysql":    {DialectMySQL, query},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := base{dialect: tc.dialect}.rebind(query)
			if got != tc.want {
				t.Errorf("rebind(%s):\n got: %s\nwant: %s", tc.dialect, got, tc.want)
			}
		})
	}
}

func TestRebindNumbersPlaceholdersPastNine(t *testing.T) {
	query := "INSERT INTO t VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"
	want := "INSERT INTO t VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)"

	if got := (base{dialect: DialectPostgres}).rebind(query); got != want {
		t.Errorf("got:  %s\nwant: %s", got, want)
	}
}

// The drivers disagree on how a date column comes back, so the scanner has to
// accept every shape they produce.
func TestDateTimeScanAcceptsDriverVariants(t *testing.T) {
	want := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)

	cases := map[string]any{
		"time.Time":     want,
		"RFC3339":       "2026-07-15T00:00:00Z",
		"bare date":     "2026-07-15",
		"bytes":         []byte("2026-07-15"),
		"mysql layout":  "2026-07-15 00:00:00",
		"with timezone": "2026-07-15 00:00:00+00:00",
	}

	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			var d dateTime
			if err := d.Scan(src); err != nil {
				t.Fatalf("scan %v: %v", src, err)
			}

			if !d.Valid {
				t.Fatal("expected a valid date")
			}

			if !d.Time.Equal(want) {
				t.Errorf("got %s, want %s", d.Time, want)
			}
		})
	}
}

// A time with a fixed offset comes back the way SQLite's driver wrote it.
//
// The driver stores a time.Time as its String form, which for an offset with no
// zone name repeats the offset where the name would be: "-0500 -0500". A date
// sent to the API with an offset used to reach the database like that, and none
// of the layouts above reads it - so the row made every list it fell into
// answer 500. Nothing writes that form any more; rows already written still have
// to be readable, as the day they were written on.
func TestDateTimeScanReadsAFixedOffsetAsSQLiteWroteIt(t *testing.T) {
	for _, src := range []string{
		"2026-07-03 23:30:00 -0500 -0500",
		"2026-07-03 01:00:00 +0530 +0530",
	} {
		var d dateTime
		if err := d.Scan(src); err != nil {
			t.Errorf("scan %q: %v", src, err)

			continue
		}

		if y, m, day := d.Time.Date(); y != 2026 || m != time.July || day != 3 {
			t.Errorf("%q read as %s, not the 3rd of July it was written on", src, d.Time)
		}
	}
}

// A time a driver hands back comes out in UTC, whatever zone it was read in.
//
// A stored day is midnight UTC, and both server dialects return it in a zone
// that is not the application's choice: lib/pq in the PostgreSQL server's
// session zone, and GoFr's MySQL connection in the process's own (loc=Local).
// West of UTC that is the evening before - and a day read in its own zone is
// the evening's day, so every entry showed on the day before it was booked.
func TestDateTimeScanReturnsADriversTimeInUTC(t *testing.T) {
	newYork := time.FixedZone("EDT", -4*60*60)
	stored := time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)

	var d dateTime
	if err := d.Scan(stored.In(newYork)); err != nil {
		t.Fatalf("scan: %v", err)
	}

	if d.Time.Location() != time.UTC {
		t.Errorf("came back in %s rather than UTC", d.Time.Location())
	}

	if y, m, day := d.Time.Date(); y != 2026 || m != time.July || day != 3 {
		t.Errorf("the 3rd of July read as %s", d.Time.Format(time.DateOnly))
	}
}

func TestDateTimeScanNullIsNotValid(t *testing.T) {
	var d dateTime
	if err := d.Scan(nil); err != nil {
		t.Fatalf("scan nil: %v", err)
	}

	if d.Valid {
		t.Error("NULL must not produce a valid date")
	}

	if ptr(d) != nil {
		t.Error("ptr() must return nil for an invalid date")
	}
}
