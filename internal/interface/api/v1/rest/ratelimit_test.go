package rest_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/interface/api/v1/rest"
)

// limited drives count sign-in attempts through the limiter and reports how
// many were refused.
//
// The path is /auth/login because that is one of the two the limiter guards at
// all; anything else is waved through and would prove nothing.
func limited(t *testing.T, handler http.Handler, count int, remoteAddr string, forwarded func(int) string) int {
	t.Helper()

	refused := 0

	for i := range count {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
		r.RemoteAddr = remoteAddr

		if forwarded != nil {
			r.Header.Set("X-Forwarded-For", forwarded(i))
		}

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code == http.StatusTooManyRequests {
			refused++
		}
	}

	return refused
}

// passes is a handler that records nothing and answers 200.
func passes() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

// The regression this file exists for.
//
// X-Forwarded-For was believed whoever sent it, so a caller inventing a fresh
// one per attempt was given a fresh budget per attempt and never met the limit
// at all. That is the whole of the brute-force defence on signing in: there is
// no account lockout behind it.
func TestAForgedForwardedHeaderDoesNotBuyAFreshBudget(t *testing.T) {
	t.Parallel()

	limiter := rest.NewRateLimiter(3, time.Minute).Middleware()(passes())

	// Ten attempts from one address off the network, each naming a different
	// client. Only the connection they arrived on is worth believing.
	refused := limited(t, limiter, 10, "203.0.113.9:5555", func(i int) string {
		return "198.51.100." + strconv.Itoa(i)
	})

	if refused != 7 {
		t.Errorf("a forged X-Forwarded-For bought %d extra attempts past the limit of 3: "+
			"want 7 of 10 refused, got %d", 7-refused, refused)
	}
}

// The other half of the same rule: a proxy this process actually put in front
// of itself has to keep working.
//
// The built-in HTTPS front end dials the plain port over loopback, so a request
// from loopback carrying X-Forwarded-For is the one case where the header is
// the only way to tell two clients apart.
func TestTheForwardedAddressIsBelievedFromTheFrontEnd(t *testing.T) {
	t.Parallel()

	limiter := rest.NewRateLimiter(3, time.Minute).Middleware()(passes())

	refused := limited(t, limiter, 10, "127.0.0.1:5555", func(i int) string {
		return "198.51.100." + strconv.Itoa(i)
	})

	if refused != 0 {
		t.Errorf("ten different clients behind the front end share one budget: "+
			"want none refused, got %d", refused)
	}
}

// And a proxy somewhere else on the network, which is what a container
// deployment looks like: believed once the operator has said where it is.
func TestATrustedProxyIsBelieved(t *testing.T) {
	t.Parallel()

	limiter := rest.NewRateLimiter(3, time.Minute).
		WithTrustedProxies([]string{"10.0.0.0/8"}).
		Middleware()(passes())

	refused := limited(t, limiter, 10, "10.1.2.3:5555", func(i int) string {
		return "198.51.100." + strconv.Itoa(i)
	})

	if refused != 0 {
		t.Errorf("a configured proxy was not believed: want none refused, got %d", refused)
	}
}

// Naming a proxy does not make every other address trustworthy.
func TestNamingAProxyDoesNotBelieveEverybodyElse(t *testing.T) {
	t.Parallel()

	limiter := rest.NewRateLimiter(3, time.Minute).
		WithTrustedProxies([]string{"10.0.0.0/8"}).
		Middleware()(passes())

	refused := limited(t, limiter, 10, "203.0.113.9:5555", func(i int) string {
		return "198.51.100." + strconv.Itoa(i)
	})

	if refused != 7 {
		t.Errorf("an address outside the trusted range was believed: want 7 refused, got %d", refused)
	}
}

// Without any header the connection is all there is, and one client still gets
// one budget.
func TestWithoutAForwardedHeaderTheConnectionIsTheClient(t *testing.T) {
	t.Parallel()

	limiter := rest.NewRateLimiter(3, time.Minute).Middleware()(passes())

	if refused := limited(t, limiter, 10, "203.0.113.9:5555", nil); refused != 7 {
		t.Errorf("want 7 of 10 refused, got %d", refused)
	}
}
