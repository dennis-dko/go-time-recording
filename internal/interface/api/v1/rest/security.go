package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
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
		// An administrator's logo is stored as a data URI, so images need that
		// scheme. The favicon used to be one as well and is a file now, which
		// narrows what this is for without removing the need for it.
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

	// current, when set, supplies the administered budget instead of the one
	// configured at start-up, so a change applies without a restart.
	current func(ctx context.Context) (int, time.Duration)
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

// WithLimits makes the budget follow the Settings screen.
//
// Applied per request rather than cached in the struct, because the point of
// administering it is that a change takes effect at once - and the provider
// behind it already caches, so this stays cheap.
func (l *RateLimiter) WithLimits(current func(ctx context.Context) (int, time.Duration)) *RateLimiter {
	l.current = current

	return l
}

// budget reports the limit and window in force for this request.
func (l *RateLimiter) budget(ctx context.Context) (int, time.Duration) {
	if l.current == nil {
		return l.limit, l.window
	}

	limit, window := l.current(ctx)
	if limit <= 0 || window <= 0 {
		// A nonsensical administered value must not switch the limiter off.
		return l.limit, l.window
	}

	return limit, window
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

			limit, window := l.budget(r.Context())

			if !l.allow(clientKey(r), limit, window) {
				seconds := int(window.Seconds())

				w.Header().Set("Retry-After", itoa(seconds))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)

				// Named, and carrying how long to wait. Retry-After already says
				// it, and a header is not something a screen can put into a
				// sentence - "try again in a minute" is the whole of what the
				// person held up by this wants to know.
				body, err := json.Marshal(map[string]any{
					"error": map[string]any{
						"message": "too many requests, please slow down",
						"code":    apperror.CodeRateLimited,
						"values":  []any{seconds},
					},
				})
				if err != nil {
					return
				}

				_, _ = w.Write(append(body, '\n'))

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

func (l *RateLimiter) allow(key string, limit int, window time.Duration) bool {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	l.collect(now)

	b, ok := l.hits[key]
	if !ok || now.After(b.reset) {
		l.hits[key] = &bucket{count: 1, reset: now.Add(window)}

		return true
	}

	if b.count >= limit {
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

// What a request may carry.
//
// Nothing bounded this. GoFr decodes the body into a struct with no cap, and
// http.Server bounds only the headers, so a single POST could name a body of any
// size and be given the memory to hold it - on /auth/login, which needs no
// session, no permission and no account that exists.
//
// The two numbers are chosen from what the application actually sends. A saved
// appearance is the largest ordinary body there is: a logo and a tab icon, each
// capped at 256 KB as a data URI, plus a title per language. That is well under
// a megabyte, and two leaves room for the next field without leaving room for an
// attack. A spreadsheet is the one thing legitimately larger, and thirty-two
// megabytes is more rows than this application will ever be handed at once.
const (
	maxRequestBody = 2 << 20
	maxImportBody  = 32 << 20
)

// LimitRequestBody refuses a body larger than the endpoint has any use for.
//
// Two checks, because they catch different callers. Content-Length is what an
// honest client declares, and answering it refuses the request before a byte of
// the body is read - which is the whole point, and gives a named refusal the
// interface can put into a sentence. MaxBytesReader is for the client that
// declares nothing, or lies: a chunked body has no length to check, so the cap
// has to sit on the reading as well, where it stops the read rather than the
// request.
func LimitRequestBody() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			allowed := int64(maxRequestBody)
			if strings.HasSuffix(r.URL.Path, "/import") {
				allowed = maxImportBody
			}

			if r.ContentLength > allowed {
				rejectTooLarge(w, allowed)

				return
			}

			r.Body = http.MaxBytesReader(w, r.Body, allowed)

			next.ServeHTTP(w, r)
		})
	}
}

// rejectTooLarge answers in the shape every other refusal in this API uses, and
// carries the limit so the sentence can name it.
func rejectTooLarge(w http.ResponseWriter, allowed int64) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusRequestEntityTooLarge)

	body, err := json.Marshal(map[string]any{
		"error": map[string]any{
			"message": "the request body is too large",
			"code":    apperror.CodeBodyTooLarge,
			"values":  []any{allowed / (1 << 20)},
		},
	})
	if err != nil {
		// The map above cannot fail to encode, and a refusal with no body is
		// still a refusal.
		return
	}

	_, _ = w.Write(append(body, '\n'))
}
