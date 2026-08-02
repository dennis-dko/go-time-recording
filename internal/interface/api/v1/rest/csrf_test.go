package rest_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/interface/api/v1/rest"
)

// reached marks that the request got past the middleware.
const reached = "HANDLER REACHED"

func csrfServer() http.Handler {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(reached))
	})

	return rest.CSRFMiddleware()(next)
}

// send runs one request through the middleware.
func send(r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	csrfServer().ServeHTTP(rec, r)

	return rec
}

// tokenFor performs the visit a browser makes before anything else and returns
// the token it was handed.
func tokenFor(t *testing.T) string {
	t.Helper()

	rec := send(httptest.NewRequest(http.MethodGet, "http://gtr.example.com/", nil))

	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == rest.CSRFCookieName {
			return cookie.Value
		}
	}

	t.Fatal("a plain visit must hand out a CSRF token")

	return ""
}

// post builds a state-changing request the way the interface makes one.
func post(token, origin string, sendHeader bool) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "http://gtr.example.com/api/v1/timesheets", nil)

	if origin != "" {
		r.Header.Set("Origin", origin)
	}

	if token != "" {
		r.AddCookie(&http.Cookie{Name: rest.CSRFCookieName, Value: token})

		if sendHeader {
			r.Header.Set(rest.CSRFHeaderName, token)
		}
	}

	return r
}

// Reading is unaffected, and a first visit leaves the browser holding a token.
func TestSafeMethodsPassAndReceiveAToken(t *testing.T) {
	rec := send(httptest.NewRequest(http.MethodGet, "http://gtr.example.com/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("a GET must not be refused, got %d", rec.Code)
	}

	if rec.Body.String() != reached {
		t.Error("the handler should have run")
	}

	if tokenFor(t) == "" {
		t.Error("expected a token to be issued")
	}
}

// The token must be readable by our own script, or it could never be echoed.
func TestTokenCookieIsReadableByScriptButTheSessionIsNot(t *testing.T) {
	rec := send(httptest.NewRequest(http.MethodGet, "http://gtr.example.com/", nil))

	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name != rest.CSRFCookieName {
			continue
		}

		if cookie.HttpOnly {
			t.Error("the CSRF cookie must be readable by script")
		}

		if cookie.SameSite != http.SameSiteLaxMode {
			t.Error("expected SameSite=Lax on the CSRF cookie")
		}
	}
}

// The ordinary path: our own page, cookie and header agreeing.
func TestMatchingTokenFromOurOwnOriginIsAccepted(t *testing.T) {
	token := tokenFor(t)

	rec := send(post(token, "http://gtr.example.com", true))
	if rec.Code != http.StatusOK {
		t.Fatalf("a legitimate request must pass, got %d: %s", rec.Code, rec.Body.String())
	}
}

// The attack this exists to stop: a page on another site making the browser
// send its session cookie along with a state-changing request.
func TestPostFromAnotherSiteIsRefused(t *testing.T) {
	token := tokenFor(t)

	// The attacker's page cannot read the cookie, so it cannot set the header;
	// the browser still attaches the cookie itself.
	rec := send(post(token, "http://evil.example.net", false))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a cross-site post must be refused, got %d", rec.Code)
	}

	if rec.Body.String() == reached {
		t.Error("the handler must not have run")
	}
}

// Even if the token leaked, the origin check still turns the request away.
func TestCrossSitePostWithAKnownTokenIsStillRefused(t *testing.T) {
	token := tokenFor(t)

	rec := send(post(token, "http://evil.example.net", true))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a cross-site post must be refused even with the token, got %d", rec.Code)
	}
}

// A request that declares no origin at all is not assumed friendly.
func TestPostWithoutAnOriginIsRefused(t *testing.T) {
	token := tokenFor(t)

	rec := send(post(token, "", true))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a request with no Origin or Referer must be refused, got %d", rec.Code)
	}
}

// Same origin, but the header was never sent: the hallmark of a forged form.
func TestPostWithoutTheHeaderIsRefused(t *testing.T) {
	token := tokenFor(t)

	rec := send(post(token, "http://gtr.example.com", false))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a request without the header must be refused, got %d", rec.Code)
	}
}

// A guessed token must not work.
func TestPostWithAMismatchedTokenIsRefused(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "http://gtr.example.com/api/v1/timesheets", nil)
	r.Header.Set("Origin", "http://gtr.example.com")
	r.AddCookie(&http.Cookie{Name: rest.CSRFCookieName, Value: tokenFor(t)})
	r.Header.Set(rest.CSRFHeaderName, "not-the-right-token")

	if rec := send(r); rec.Code != http.StatusForbidden {
		t.Fatalf("a mismatched token must be refused, got %d", rec.Code)
	}
}

// Scripts authenticate with a header a browser never sends by itself, so there
// is nothing to forge and nothing to demand.
func TestAPITokenRequestsAreExempt(t *testing.T) {
	for _, header := range []struct{ name, value string }{
		{rest.APITokenHeader, "gtr_something"},
		{"Authorization", "Bearer gtr_something"},
	} {
		t.Run(header.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "http://gtr.example.com/api/v1/timesheets", nil)
			r.Header.Set(header.name, header.value)

			rec := send(r)
			if rec.Code != http.StatusOK {
				t.Fatalf("a token-authenticated call must not need a CSRF token, got %d: %s",
					rec.Code, rec.Body.String())
			}
		})
	}
}

// Every method that changes something is covered, not just POST.
func TestAllStateChangingMethodsAreChecked(t *testing.T) {
	for _, method := range []string{
		http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete,
	} {
		t.Run(method, func(t *testing.T) {
			r := httptest.NewRequest(method, "http://gtr.example.com/api/v1/timesheets", nil)
			r.Header.Set("Origin", "http://evil.example.net")
			r.AddCookie(&http.Cookie{Name: rest.CSRFCookieName, Value: tokenFor(t)})

			if rec := send(r); rec.Code != http.StatusForbidden {
				t.Errorf("%s from another site must be refused, got %d", method, rec.Code)
			}
		})
	}
}

// A deployment behind a proxy that rewrites the host must still recognise its
// own origin, or every write would fail once TLS is terminated in front.
func TestForwardedHostIsHonoured(t *testing.T) {
	token := tokenFor(t)

	r := httptest.NewRequest(http.MethodPost, "http://internal-backend:8000/api/v1/timesheets", nil)
	r.Header.Set("X-Forwarded-Host", "gtr.example.com")
	r.Header.Set("Origin", "https://gtr.example.com")
	r.AddCookie(&http.Cookie{Name: rest.CSRFCookieName, Value: token})
	r.Header.Set(rest.CSRFHeaderName, token)

	if rec := send(r); rec.Code != http.StatusOK {
		t.Fatalf("a proxied request from the public host must pass, got %d: %s",
			rec.Code, rec.Body.String())
	}
}

// Referer stands in where Origin was stripped.
func TestRefererIsAcceptedWhenOriginIsAbsent(t *testing.T) {
	token := tokenFor(t)

	r := httptest.NewRequest(http.MethodPost, "http://gtr.example.com/api/v1/timesheets", nil)
	r.Header.Set("Referer", "http://gtr.example.com/some/page")
	r.AddCookie(&http.Cookie{Name: rest.CSRFCookieName, Value: token})
	r.Header.Set(rest.CSRFHeaderName, token)

	if rec := send(r); rec.Code != http.StatusOK {
		t.Fatalf("a same-site Referer must be accepted, got %d: %s", rec.Code, rec.Body.String())
	}
}

// Two visitors must not be handed the same token.
func TestTokensAreUnique(t *testing.T) {
	seen := map[string]bool{}

	for range 50 {
		token := tokenFor(t)

		if token == "" {
			t.Fatal("an empty token was issued")
		}

		if seen[token] {
			t.Fatalf("token %q was issued twice", token)
		}

		seen[token] = true
	}
}
