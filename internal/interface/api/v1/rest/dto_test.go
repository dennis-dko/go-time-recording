package rest

import (
	"encoding/json"
	"math"
	"strconv"
	"testing"
	"time"
)

// An id too large to hold is refused rather than wrapped.
//
// parseUint returns a uint, which is 64 bits on everything this ships for and
// 32 on something it does not. Read at 64 and converted afterwards, a value past
// four billion would arrive there as a different, valid-looking id and the
// request would be answered - about the wrong row. It is read at the
// destination's own width now, so the refusal happens in the reading.
func TestAnIdTooLargeToHoldIsRefused(t *testing.T) {
	// Only where it can be: on a 64-bit platform a uint holds everything a
	// 64-bit read can produce, so there is no number to refuse and nothing to
	// prove. The guard exists for the platform where there is - and saying so
	// here is better than an assertion that quietly cannot fail, which is
	// precisely what the first version of the guard itself turned out to be.
	if strconv.IntSize < 64 {
		tooLarge := strconv.FormatUint(math.MaxUint64, 10)

		if _, err := parseUint(tooLarge, "id"); err == nil {
			t.Errorf("%s was accepted, which on this platform is a different row",
				tooLarge)
		}
	}

	// And the ordinary ones still work, so this is a ceiling rather than a wall.
	for _, ok := range []string{"1", "42", "4294967295"} {
		if got, err := parseUint(ok, "id"); err != nil {
			t.Errorf("%s was refused: %v", ok, err)
		} else if strconv.FormatUint(uint64(got), 10) != ok {
			t.Errorf("%s came back as %d", ok, got)
		}
	}

	// Zero and nonsense stay refused, which is what this did before.
	for _, bad := range []string{"0", "", "-1", "x"} {
		if _, err := parseUint(bad, "id"); err == nil {
			t.Errorf("%q was accepted as an id", bad)
		}
	}
}

// A date is the day it names, whatever form it arrives in.
//
// A full timestamp carries a time of day and an offset, and both used to reach
// the database: SQLite then wrote "2026-07-03 23:30:00 -0500 -0500", which it
// could not read back, and PostgreSQL and MySQL stored the instant, which is the
// 4th in UTC. The day is the one written, so every form becomes midnight UTC of
// it - the one shape a stored date has.
func TestADateIsTheDayItNames(t *testing.T) {
	want := time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)

	for _, sent := range []string{
		`"2026-07-03"`,
		`"2026-07-03T01:00:00+02:00"`,
		`"2026-07-03T23:30:00-05:00"`,
		`"2026-07-03T12:00:00.5Z"`,
	} {
		var d Date

		if err := json.Unmarshal([]byte(sent), &d); err != nil {
			t.Errorf("%s was refused: %v", sent, err)

			continue
		}

		if !d.Time.Equal(want) || d.Location() != time.UTC {
			t.Errorf("%s became %s, want %s", sent, d.Time, want)
		}
	}
}
