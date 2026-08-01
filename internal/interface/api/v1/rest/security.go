package rest

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

// SecurityHeaders sets the response headers that a browser needs in order to
// defend the interface, and stops the API from being cached or framed.
//
// The Content-Security-Policy is strict because the whole interface is served
// from this binary: there is no CDN, no inline script and no external font, so
// nothing legitimate is blocked by refusing them.
func SecurityHeaders(hstsMaxAge time.Duration) func(http.Handler) http.Handler {
	csp := strings.Join([]string{
		"default-src 'self'",
		// The favicon is an inline SVG data URI, and the logo is stored as a
		// data URI, so images need that scheme.
		"img-src 'self' data:",
		"script-src 'self'",
		"style-src 'self'",
		"connect-src 'self'",
		"font-src 'self'",
		"form-action 'self'",
		"base-uri 'none'",
		"frame-ancestors 'none'",
		"object-src 'none'",
	}, "; ")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := w.Header()

			header.Set("Content-Security-Policy", csp)
			// Legacy equivalent of frame-ancestors, for browsers that predate CSP 2.
			header.Set("X-Frame-Options", "DENY")
			header.Set("X-Content-Type-Options", "nosniff")
			header.Set("Referrer-Policy", "no-referrer")
			header.Set("Cross-Origin-Opener-Policy", "same-origin")
			header.Set("Cross-Origin-Resource-Policy", "same-origin")
			// The application needs none of these, so they are switched off.
			header.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), interest-cohort=()")

			// Responses carry working time and personal data; a shared cache
			// must never keep them.
			if strings.HasPrefix(r.URL.Path, "/api/") {
				header.Set("Cache-Control", "no-store")
				header.Set("Pragma", "no-cache")
			}

			// HSTS is only meaningful, and only safe, over a connection that
			// already is HTTPS: sending it over plain HTTP would let a single
			// intercepted response lock a host out of working at all.
			if hstsMaxAge > 0 && isTLS(r) {
				header.Set("Strict-Transport-Security",
					"max-age="+itoa(int(hstsMaxAge.Seconds()))+"; includeSubDomains")
			}

			next.ServeHTTP(w, r)
		})
	}
}

// itoa avoids pulling strconv in for one call.
func itoa(v int) string {
	if v == 0 {
		return "0"
	}

	var buf [20]byte

	i := len(buf)

	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}

	return string(buf[i:])
}

// RateLimiter throttles requests per client.
//
// It exists mainly to blunt password and token guessing: without it an
// attacker can try credentials as fast as the network allows. It is a
// per-process limiter, so several instances behind a load balancer each get
// their own budget - a reverse proxy is still the right place for a hard
// global limit.
type RateLimiter struct {
	mu      sync.Mutex
	hits    map[string]*bucket
	limit   int
	window  time.Duration
	lastGC  time.Time
	gcEvery time.Duration
}

type bucket struct {
	count int
	reset time.Time
}

// NewRateLimiter allows limit requests per window per client.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		hits:    make(map[string]*bucket),
		limit:   limit,
		window:  window,
		lastGC:  time.Now(),
		gcEvery: window * 10,
	}
}

// Middleware applies the limit to the paths where guessing actually pays off:
// signing in and anything carrying an API token. Ordinary browsing by a
// signed-in user is left alone so a busy day cannot lock someone out.
func (l *RateLimiter) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !l.guards(r) {
				next.ServeHTTP(w, r)

				return
			}

			if !l.allow(clientKey(r)) {
				w.Header().Set("Retry-After", itoa(int(l.window.Seconds())))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":{"message":"too many requests, please slow down"}}` + "\n"))

				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// guards reports whether this request is worth limiting.
func (l *RateLimiter) guards(r *http.Request) bool {
	if strings.HasSuffix(r.URL.Path, "/auth/login") {
		return true
	}

	// Token holders are scripts; a browser session is already rate-limited by
	// the human driving it.
	return r.Header.Get(APITokenHeader) != "" ||
		strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ")
}

func (l *RateLimiter) allow(key string) bool {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	l.collect(now)

	b, ok := l.hits[key]
	if !ok || now.After(b.reset) {
		l.hits[key] = &bucket{count: 1, reset: now.Add(l.window)}

		return true
	}

	if b.count >= l.limit {
		return false
	}

	b.count++

	return true
}

// collect drops expired buckets so the map cannot grow without bound.
func (l *RateLimiter) collect(now time.Time) {
	if now.Sub(l.lastGC) < l.gcEvery {
		return
	}

	l.lastGC = now

	for key, b := range l.hits {
		if now.After(b.reset) {
			delete(l.hits, key)
		}
	}
}

// clientKey identifies the caller for rate limiting.
//
// X-Forwarded-For is only consulted for its last hop, which is the address the
// nearest proxy observed; the earlier entries are client-supplied and trivial
// to forge.
func clientKey(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		parts := strings.Split(forwarded, ",")

		return strings.TrimSpace(parts[len(parts)-1])
	}

	host := r.RemoteAddr
	if idx := strings.LastIndex(host, ":"); idx > 0 {
		host = host[:idx]
	}

	return host
}
