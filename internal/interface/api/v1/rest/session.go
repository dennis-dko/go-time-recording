package rest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"slices"
	"strings"
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
)

// SessionCookieName is the cookie carrying the session token.
const SessionCookieName = "gtr_session"

// sessionContextKey types the context value so it cannot collide with a key
// from another package.
type sessionContextKey struct{}

// SessionMiddleware resolves the session cookie and puts the principal on the
// request context, where the Authorizer picks it up.
//
// It never rejects a request itself: an anonymous request simply arrives
// without a principal, and the handlers decide what that means. That keeps the
// login endpoint and the UI assets reachable without a special case list here.
func SessionMiddleware(sessions *service.SessionService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(SessionCookieName)
			if err != nil || cookie.Value == "" {
				next.ServeHTTP(w, r)

				return
			}

			principal, err := sessions.Resolve(r.Context(), cookie.Value)
			if err != nil {
				// A stale cookie is cleared so the browser stops sending it.
				http.SetCookie(w, expiredCookie(r))
				next.ServeHTTP(w, r)

				return
			}

			// On every answer, so a change of rights is noticed on the next request
			// rather than at the next sign-in.
			//
			// The server has always enforced the change immediately - the principal
			// above is resolved from the database per request - but nothing told the
			// browser, which had read /me once at start-up and kept the tabs and
			// buttons it was given. So a revoked right showed up as a refusal on a
			// control that was still on screen, with no explanation attached.
			w.Header().Set(PermissionRevisionHeader, PermissionRevision(principal))

			ctx := context.WithValue(r.Context(), sessionContextKey{}, principal)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// PermissionRevisionHeader carries what the caller may currently do, as a value
// that changes only when the answer does.
const PermissionRevisionHeader = "X-Permissions-Revision"

// PermissionRevision is a short stable digest of what a principal may do.
//
// A digest rather than a counter, because there is no one place a change to a
// role, a role assignment or the permission list passes through - and a counter
// that something forgets to bump is worse than none. Anything that changes what
// this account may do changes these inputs, and nothing else does.
//
// Sorted first, so the same rights in a different order are the same revision and
// the interface is not told to reload for a row order.
func PermissionRevision(principal *service.Principal) string {
	if principal == nil || principal.User == nil {
		return ""
	}

	permissions := slices.Clone(principal.Permissions)
	slices.Sort(permissions)

	sum := sha256.Sum256([]byte(principal.User.RoleName + "\x00" + strings.Join(permissions, "\x00")))

	// Half the digest. This is a change detector, not a secret: it is compared
	// with itself, and a collision costs a reload nobody needed.
	return hex.EncodeToString(sum[:6])
}

// principalFromContext returns the signed-in caller, if any.
func principalFromContext(ctx context.Context) (*service.Principal, bool) {
	principal, ok := ctx.Value(sessionContextKey{}).(*service.Principal)

	return principal, ok
}

// sessionCookie builds the cookie for a newly opened session.
func sessionCookie(r *http.Request, token string, expires time.Time) *http.Cookie {
	return &http.Cookie{
		Name:  SessionCookieName,
		Value: token,
		Path:  "/",
		// HttpOnly keeps the token away from JavaScript, so an injected script
		// cannot read it. SameSite=Lax stops other sites from riding on the
		// session for state-changing requests.
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isTLS(r),
		Expires:  expires,
	}
}

// expiredCookie clears the session cookie.
func expiredCookie(r *http.Request) *http.Cookie {
	return &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isTLS(r),
		MaxAge:   -1,
	}
}

// isTLS reports whether the request reached us over HTTPS, including through a
// terminating proxy. Setting Secure on a plain-HTTP deployment would make the
// browser drop the cookie and nobody could sign in.
func isTLS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}

	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// setCookie writes a cookie on the response behind a GoFr context.
//
// GoFr hides the ResponseWriter from handlers, so the cookie is queued on the
// request context and written by the middleware that wraps every response.
func setCookie(c *gofr.Context, cookie *http.Cookie) {
	if queue, ok := c.Request.Context().Value(cookieQueueKey{}).(*cookieQueue); ok {
		queue.add(cookie)
	}
}

type cookieQueueKey struct{}

// requestKey carries the raw *http.Request. GoFr's Context exposes only its
// own Request abstraction, which has no access to cookies or TLS state.
type requestKey struct{}

// requestOf returns the raw request behind a GoFr context.
func requestOf(c *gofr.Context) *http.Request {
	req, _ := c.Request.Context().Value(requestKey{}).(*http.Request)

	return req
}

// cookieQueue collects cookies a handler wants to send.
type cookieQueue struct {
	cookies []*http.Cookie
}

func (q *cookieQueue) add(cookie *http.Cookie) {
	q.cookies = append(q.cookies, cookie)
}

// CookieMiddleware gives handlers a way to set cookies despite GoFr owning the
// ResponseWriter. It must be registered before any handler that sets one.
func CookieMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			queue := &cookieQueue{}
			ctx := context.WithValue(r.Context(), cookieQueueKey{}, queue)
			ctx = context.WithValue(ctx, requestKey{}, r)

			// The cookies are written by wrapping the writer, because the
			// handler runs before we get control back and headers must be set
			// before the first byte of the body.
			next.ServeHTTP(&cookieWriter{ResponseWriter: w, queue: queue}, r.WithContext(ctx))
		})
	}
}

// cookieWriter flushes queued cookies just before the response head is sent.
type cookieWriter struct {
	http.ResponseWriter

	queue   *cookieQueue
	written bool
}

func (w *cookieWriter) WriteHeader(status int) {
	w.flush()
	w.ResponseWriter.WriteHeader(status)
}

func (w *cookieWriter) Write(b []byte) (int, error) {
	w.flush()

	return w.ResponseWriter.Write(b)
}

func (w *cookieWriter) flush() {
	if w.written {
		return
	}

	w.written = true

	for _, cookie := range w.queue.cookies {
		http.SetCookie(w.ResponseWriter, cookie)
	}
}
