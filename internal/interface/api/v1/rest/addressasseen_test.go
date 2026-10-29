package rest

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The address put into a code for another device is the one the page shows: the
// Origin its browser sent, where that names the host the request reached - so a
// proxy in front that does not say which scheme the browser used cannot turn the
// page's https into http - and the address as reached otherwise.
func TestTheAddressACodeCarriesIsTheOneThePageShows(t *testing.T) {
	for _, tc := range []struct {
		name    string
		host    string
		headers map[string]string
		want    string
	}{
		{"the page's own origin", "time.example.com", map[string]string{"Origin": "https://time.example.com"},
			"https://time.example.com"},
		{"an origin naming another host", "time.example.com", map[string]string{"Origin": "https://elsewhere.example"},
			"http://time.example.com"},
		{"an origin of null", "time.example.com", map[string]string{"Origin": "null"}, "http://time.example.com"},
		{"no origin, behind a proxy that says https", "time.example.com",
			map[string]string{"X-Forwarded-Proto": "https"}, "https://time.example.com"},
		{"no origin, a port", "time.example.com:8000", nil, "http://time.example.com:8000"},
	} {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/me/tokens", nil)
		request.Host = tc.host

		for name, value := range tc.headers {
			request.Header.Set(name, value)
		}

		if got := addressAsSeen(request); got != tc.want {
			t.Errorf("%s: the address is %q, want %q", tc.name, got, tc.want)
		}
	}
}
