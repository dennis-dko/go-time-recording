package service

import (
	"testing"
	"time"
)

// An address whose domain is one label is an address.
//
// The rule required a dot in it, which reads as the obvious shape and is wrong
// on exactly the networks this application is most often installed on. "@local"
// and a bare host name are ordinary there - and the account the application
// creates for itself is admin@local, so the screen refused to create the kind of
// address it had already made.
//
// Still not a full validation, which no pattern can be: what is rejected here is
// input that is obviously not an address, and delivery is the real test.
func TestAnAddressMayHaveASingleLabelDomain(t *testing.T) {
	for _, address := range []string{
		"anna@local",
		"anna.weber@local",
		"admin@local",
		"anna@intranet",
		"anna@example.com",
		"anna@mail.example.co.uk",
	} {
		if !validEmail(address) {
			t.Errorf("%q was refused as an address", address)
		}
	}

	// And the obvious non-addresses still are.
	for _, address := range []string{
		"",
		"anna",
		"@local",
		"anna@",
		"anna@local.",
		"anna@.local",
		"anna weber@local",
		"anna@@local",
	} {
		if validEmail(address) {
			t.Errorf("%q was accepted as an address", address)
		}
	}
}

// How often a session's use is written down follows the timeout it is measured
// against.
//
// The first attempt was a flat minute, on the reasoning that a write per request
// is too many and no idle timeout worth setting is finer than that. The second
// half is true of what an administrator can set - the screen refuses anything
// under five minutes - and not of what an installation can put in its
// environment, which is where a first trial of the feature goes.
//
// With a timeout shorter than the interval, a session in constant use is never
// written down at all: it goes on looking untouched since the sign-in and is
// ended while somebody is working in it. The case for the feature found it.
func TestHowOftenUseIsWrittenDownFollowsTheTimeout(t *testing.T) {
	for name, tc := range map[string]struct {
		idle time.Duration
		want time.Duration
	}{
		"none set":       {idle: 0, want: maxTouchInterval},
		"negative":       {idle: -time.Hour, want: maxTouchInterval},
		"an office hour": {idle: time.Hour, want: maxTouchInterval},
		"thirty minutes": {idle: 30 * time.Minute, want: maxTouchInterval},
		// Half of five minutes is longer than the cap, so the cap decides.
		"the screen's": {idle: 5 * time.Minute, want: maxTouchInterval},
		// And here half is shorter than the cap, so half decides.
		"a minute":       {idle: time.Minute, want: 30 * time.Second},
		"a trial's":      {idle: 3 * time.Second, want: 1500 * time.Millisecond},
		"absurdly short": {idle: time.Millisecond, want: 500 * time.Microsecond},
	} {
		if got := touchInterval(tc.idle); got != tc.want {
			t.Errorf("%s (%s): writes at most every %s, want %s", name, tc.idle, got, tc.want)
		}
	}

	// The property the numbers above are there to protect: a session used more
	// often than the timeout is never allowed to look untouched for it.
	for _, idle := range []time.Duration{
		time.Second, 3 * time.Second, time.Minute, 5 * time.Minute, time.Hour,
	} {
		if interval := touchInterval(idle); interval >= idle {
			t.Errorf("with a timeout of %s, use is written down every %s - so a "+
				"session in constant use is ended while somebody is in it", idle, interval)
		}
	}
}
