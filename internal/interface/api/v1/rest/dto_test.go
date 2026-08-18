package rest

import (
	"math"
	"strconv"
	"testing"
)

// An id too large to hold is refused rather than wrapped.
//
// parseUint reads into 64 bits and returns a uint, which is 64 bits on
// everything this ships for and 32 on something it does not. There the
// conversion wraps silently: an id past four billion comes back as a different
// id that looks perfectly valid, so the request is answered - about the wrong
// row. A refusal is the only honest answer to a number this code cannot hold.
func TestAnIdTooLargeToHoldIsRefused(t *testing.T) {
	// Only where it can be: on a 64-bit platform a uint holds everything
	// ParseUint can produce, so there is no number to refuse and nothing to
	// prove. The check exists for the platform where there is - and saying so
	// here is better than an assertion that quietly cannot fail.
	if math.MaxUint < math.MaxUint64 {
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
