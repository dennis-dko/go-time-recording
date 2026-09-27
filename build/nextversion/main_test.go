package main

import "testing"

// Each part counts to 99 and then hands on to the one before it.
//
// Counting the patch without a bound produced v0.2.100: a number people have to
// read carefully, and one that says "a hundred fixes into this line" where what
// has happened is that the line went on longer than any line should. After
// x.y.99 comes x.(y+1).0, and after x.99.99 comes (x+1).0.0 - and a tag already
// past the bound, as v0.2.100 is, hands on the same way rather than going to 101.
func TestTheNextVersionRollsOverAtNinetyNine(t *testing.T) {
	for latest, want := range map[string]string{
		"":         "v0.1.0",
		"v0.2.0":   "v0.2.1",
		"v0.2.98":  "v0.2.99",
		"v0.2.99":  "v0.3.0",
		"v0.2.100": "v0.3.0",
		"v0.98.99": "v0.99.0",
		"v0.99.99": "v1.0.0",
		"v1.4.7":   "v1.4.8",
	} {
		got, err := next(latest)
		if err != nil {
			t.Errorf("next(%q) failed: %v", latest, err)

			continue
		}

		if got != want {
			t.Errorf("after %q comes %q, want %q", latest, got, want)
		}
	}
}

// Anything that is not a plain version is refused rather than guessed at, because
// a release named from a misread tag is published before anybody looks.
func TestSomethingThatIsNotAVersionIsRefused(t *testing.T) {
	for _, latest := range []string{"0.2.1", "v0.2", "v0.2.1-rc1", "v0.x.1", "v-1.0.0"} {
		if got, err := next(latest); err == nil {
			t.Errorf("next(%q) answered %q rather than refusing", latest, got)
		}
	}
}
