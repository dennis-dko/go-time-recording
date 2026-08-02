package rest

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// CSRFCookieName carries the token the browser must echo back. Unlike the
// session cookie this one is deliberately readable by JavaScript: the whole
// point is that our own script can read it and another site's cannot.
const CSRFCookieName = "gtr_csrf"

// CSRFHeaderName is where the interface returns the token.
//
// A header rather than a form field, because a cross-site form post cannot set
// custom headers, and anything that could would need CORS permission we never
// grant.
const CSRFHeaderName = "X-CSRF-Token" //nolint:gosec // header name, not a credential

// csrfTokenBytes is the entropy behind one token. 32 bytes is far past what
// guessing could reach within a session's lifetime.
const csrfTokenBytes = 32

// CSRFMiddleware rejects state-changing requests that a browser was tricked
// into sending from another site.
//
// The session cookie is SameSite=Lax, which already blocks the common
// cross-site form post. That is a mitigation and not a guarantee: it says
// nothing about a same-site subdomain, older browsers apply it inconsistently,
// and it would silently stop protecting anything if the cookie policy were ever
// relaxed. So two independent checks run here.
//
//  1. Origin, or failing that Referer, must name this host. Browsers always
//     send Origin on the methods that change state, and a page on another
//     origin cannot forge it.
//  2. The token in the header must equal the one in the cookie. Reading that
//     cookie requires being on this origin, which is exactly what an attacker
//     is not.
//
// Requests authenticated by a personal API token are exempt: a browser never
// attaches that header on its own, so there is nothing to forge, and demanding
// a cookie would break every script.
func CSRFMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := issuedCSRFToken(w, r)

			if !csrfApplies(r) {
				next.ServeHTTP(w, r)

				return
			}

			if reason := checkCSRF(r, token); reason != "" {
				rejectCSRF(w, reason)

				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// issuedCSRFToken returns the caller's token, minting one when the browser has
// none yet. A fresh visitor therefore already holds a usable token by the time
// the interface has loaded.
func issuedCSRFToken(w http.ResponseWriter, r *http.Request) string {
	if cookie, err := r.Cookie(CSRFCookieName); err == nil && cookie.Value != "" {
		return cookie.Value
	}

	token := newCSRFToken()
	if token == "" {
		return ""
	}

	http.SetCookie(w, csrfCookie(r, token))

	return token
}

// newCSRFToken produces one token, or an empty string if the system has no
// randomness to give - in which case every check below fails closed.
func newCSRFToken() string {
	buf := make([]byte, csrfTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}

	return base64.RawURLEncoding.EncodeToString(buf)
}

// csrfCookie builds the cookie holding the token.
func csrfCookie(r *http.Request, token string) *http.Cookie {
	return &http.Cookie{
		Name:  CSRFCookieName,
		Value: token,
		Path:  "/",
		// Readable by script on purpose; see CSRFCookieName.
		HttpOnly: false,
		SameSite: http.SameSiteLaxMode,
		Secure:   isTLS(r),
		Expires:  time.Now().Add(csrfCookieLifetime),
	}
}

// csrfCookieLifetime outlives any plausible session, so a token does not expire
// out from under someone who left a tab open over a long weekend.
const csrfCookieLifetime = 30 * 24 * time.Hour

// RotateCSRFToken issues a fresh token, which the sign-in handler does once a
// session exists so that a token handed out to an anonymous visitor is never
// carried into an authenticated one.
func RotateCSRFToken(r *http.Request) *http.Cookie {
	token := newCSRFToken()
	if token == "" {
		return nil
	}

	return csrfCookie(r, token)
}

// csrfApplies reports whether this request needs checking.
func csrfApplies(r *http.Request) bool {
	// Safe methods do not change state; requiring a token on them would only
	// break links and bookmarks.
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return false
	}

	// A personal token is never attached by the browser on its own.
	return presentedToken(r) == ""
}

// checkCSRF returns why a request is refused, or an empty string if it passes.
func checkCSRF(r *http.Request, expected string) string {
	if !sameOrigin(r) {
		return "the request came from another site"
	}

	presented := r.Header.Get(CSRFHeaderName)
	if presented == "" || expected == "" {
		return "the request carried no CSRF token"
	}

	// Constant time, so a wrong token cannot be narrowed down by timing how
	// long the comparison took.
	if subtle.ConstantTimeCompare([]byte(presented), []byte(expected)) != 1 {
		return "the CSRF token did not match"
	}

	return ""
}

// sameOrigin reports whether the request declares itself as coming from here.
//
// Origin is checked first because it is sent on exactly the methods that matter
// and cannot be set by the page making the request. Referer is the fallback for
// the few situations where Origin is stripped; a request declaring neither is
// refused rather than assumed friendly.
func sameOrigin(r *http.Request) bool {
	host := requestHost(r)
	if host == "" {
		return false
	}

	if origin := r.Header.Get("Origin"); origin != "" {
		return originHost(origin) == host
	}

	if referer := r.Header.Get("Referer"); referer != "" {
		return originHost(referer) == host
	}

	return false
}

// requestHost is the host this request was addressed to, honouring a
// terminating proxy that rewrote it.
func requestHost(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-Host"); forwarded != "" {
		// Only the first hop is the original host; later ones were added by
		// intermediaries.
		return strings.ToLower(strings.TrimSpace(strings.Split(forwarded, ",")[0]))
	}

	return strings.ToLower(r.Host)
}

// originHost pulls the host out of an Origin or Referer value.
func originHost(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return ""
	}

	return strings.ToLower(parsed.Host)
}

// rejectCSRF answers in the same error shape the rest of the API uses, so the
// interface can show the reason like any other failure.
func rejectCSRF(w http.ResponseWriter, reason string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`{"error":{"message":"` + reason + `"}}` + "\n"))
}
