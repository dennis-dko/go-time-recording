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
