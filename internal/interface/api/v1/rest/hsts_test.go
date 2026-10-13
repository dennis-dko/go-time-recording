package rest_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/interface/api/v1/rest"
)

// HSTS switched off is said to the browser, not left unsaid.
//
// A browser keeps a policy it was given until the policy runs out, so leaving
// the header out switches nothing off where it was ever sent: a browser that
// stored a year goes on refusing, for the rest of that year, any certificate it
// cannot verify, with no way past the warning (RFC 6797, 8.4). max-age=0 is how
// the same RFC tells it to forget the host (6.1.1).
func TestHSTSSwitchedOffTellsTheBrowserToForget(t *testing.T) {
	cases := map[time.Duration]string{
		0:                    "max-age=0",
		365 * 24 * time.Hour: "max-age=31536000; includeSubDomains",
	}

	for age, want := range cases {
		handler := rest.SecurityHeaders(age)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "https://time.example/", nil))

		if got := recorder.Header().Get("Strict-Transport-Security"); got != want {
			t.Errorf("HSTS_MAX_AGE=%s sends %q over HTTPS, want %q", age, got, want)
		}
	}
}
