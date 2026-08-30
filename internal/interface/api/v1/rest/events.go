package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/announce"
)

// EventsPath is where a browser opens its stream.
const EventsPath = "/api/v1/events"

// heartbeat is how often a comment is written down an idle stream.
//
// Not for the browser, which is happy to wait: for everything between. A reverse
// proxy will close a connection that has said nothing for a minute, and the
// browser then reconnects, and the whole arrangement still works - it just
// reconnects for ever for no reason. Twenty seconds is under every default worth
// naming.
const heartbeat = 20 * time.Second

// permissionCheck is how often an open stream asks whether the account holding
// it may still do what it could a moment ago.
//
// The revision travels on every response, so anybody clicking around finds out
// without this. Somebody reading a screen is not clicking around, and that is
// precisely the case worth covering: a right is withdrawn while the person it
// was withdrawn from is looking at the screen it opened. The browser polls for
// that once a minute, and a minute is a long time to leave somebody in front of
// controls that have stopped meaning anything.
//
// Derived rather than published. Nothing tells this stream that a role changed,
// because there is no one place such a change passes through - a role
// assignment, the rights on a role, and a directory synchronisation are three
// different paths - and a notification that one of them forgets to send is worse
// than none at all. It is the same reasoning that made PermissionRevision a
// digest rather than a counter.
//
// The cost is two indexed reads per open stream per interval, which is what any
// single request already does to work out who is calling. Ten seconds reads as
// "at once" to somebody watching a screen, and this is a notice rather than an
// enforcement in any case: the API refuses a withdrawn right on the very next
// call whatever the browser believes.
const permissionCheck = 10 * time.Second

// permissionsChanged is what a stream says when what its account may do has
// moved. It carries the new revision and nothing else - the browser compares it
// with the one /me gave it, and says so once.
type permissionsChanged struct {
	Revision string `json:"revision"`
}

// EventStream serves server-sent events to signed-in browsers.
//
// Middleware rather than a GoFr handler, for the same reason the interface is:
// this needs the raw ResponseWriter. A response here is not a value to be
// serialised once, it is a connection held open and written to over minutes, and
// GoFr's handler signature - return a value, return an error - is the wrong shape
// for that by construction.
//
// Placed after the session middleware, so the caller is already resolved.
func EventStream(hub *announce.Hub, auth *service.AuthService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != EventsPath {
				next.ServeHTTP(w, r)

				return
			}

			if r.Method != http.MethodGet {
				w.Header().Set("Allow", http.MethodGet)
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

				return
			}

			// Signed in, and nothing more. What is announced here is that the
			// application itself is about to restart, which is every user's
			// business and no user's secret - it carries no data belonging to
			// anybody. Requiring a permission would only mean that the people who
			// most need the warning are the ones who do not get it.
			principal, ok := principalFromContext(r.Context())
			if !ok {
				http.Error(w, "not authenticated", http.StatusUnauthorized)

				return
			}

			// Through the controller rather than a type assertion on the writer.
			// By the time a request reaches here it has been through the whole
			// middleware chain, and the writer is somebody's wrapper - GoFr's own
			// records the status for its log. A bare w.(http.Flusher) fails against
			// every one of those, and the failure is a 500 on a feature that works
			// perfectly: the controller follows Unwrap down to the connection,
			// which is what that interface is for.
			controller := http.NewResponseController(w)

			// No write deadline. A server that sets one is right to for ordinary
			// responses and wrong for this: the connection is meant to be idle for
			// minutes at a time, and a deadline would close it on schedule. Not
			// fatal if it is refused - it means there was no deadline to clear.
			_ = controller.SetWriteDeadline(time.Time{})

			header := w.Header()
			header.Set("Content-Type", "text/event-stream")
			header.Set("Cache-Control", "no-store")
			header.Set("Connection", "keep-alive")
			// Proxies that buffer responses would hold every announcement until
			// the stream closed. nginx reads this one; others read Cache-Control.
			header.Set("X-Accel-Buffering", "no")

			w.WriteHeader(http.StatusOK)

			// How long to wait before reconnecting, in milliseconds. Browsers
			// reconnect on their own; this says how eagerly. Two seconds because
			// the connection this is most likely to lose is one dropped by a
			// restart, and coming back promptly is the point.
			_, _ = w.Write([]byte("retry: 2000\n\n"))

			// The first flush is also the test of whether flushing works here at
			// all, and it has to come after the header rather than before it - a
			// flush on a writer that has had nothing written to it sends the header
			// as it stands, which was the whole set of defaults this replaces.
			//
			// Nothing to report if it fails: the status has gone out, and a stream
			// that cannot flush is a connection that will deliver everything at
			// once when it closes. Closing it now is the honest end, and the
			// browser reconnects.
			if err := controller.Flush(); err != nil {
				return
			}

			stream, unsubscribe := hub.Subscribe()
			defer unsubscribe()

			ticker := time.NewTicker(heartbeat)
			defer ticker.Stop()

			// Whose stream this is, and what they could do when it opened. Both
			// are read once: the connection belongs to that account for as long
			// as it is held, and the revision is what every later answer is
			// compared against.
			var watching uint

			revision := PermissionRevision(principal)

			if principal.User != nil {
				watching = principal.User.ID
			}

			rights := time.NewTicker(permissionCheck)
			defer rights.Stop()

			for {
				select {
				case <-r.Context().Done():
					// The browser went away, or this process is shutting down -
					// which, during an update, is precisely what is meant to
					// happen next.
					return

				case announcement, open := <-stream:
					if !open {
						return
					}

					body, err := json.Marshal(announcement)
					if err != nil {
						// Cannot happen for this type, and dropping the
						// connection over it would be worse than saying nothing.
						continue
					}

					if _, err := w.Write([]byte("event: announcement\ndata: " +
						string(body) + "\n\n")); err != nil {
						return
					}

					_ = controller.Flush()

				case <-ticker.C:
					// A comment. Browsers ignore it; everything in between counts
					// it as traffic.
					if _, err := w.Write([]byte(": keep-alive\n\n")); err != nil {
						return
					}

					_ = controller.Flush()

				case <-rights.C:
					current, answered := currentRevision(r.Context(), auth, watching)
					if !answered || current == revision {
						// Unchanged, or unanswerable for the moment - a database
						// that is briefly busy is not news, and the next tick asks
						// again. Nothing is written either way: keeping the
						// connection alive is the other timer's job.
						continue
					}

					revision = current

					body, err := json.Marshal(permissionsChanged{Revision: current})
					if err != nil {
						continue
					}

					if _, err := w.Write([]byte("event: permissions\ndata: " +
						string(body) + "\n\n")); err != nil {
						return
					}

					_ = controller.Flush()
				}
			}
		})
	}
}

// currentRevision is what the account may do at this moment, and whether that
// could be answered at all.
//
// A false is not an error worth reporting anywhere. This runs on a timer nobody
// asked for, on behalf of a browser that is not waiting for it, and the honest
// response to an account that has just been deleted, or to a database that is
// busy, is to say nothing and ask again on the next tick.
func currentRevision(ctx context.Context, auth *service.AuthService, id uint) (string, bool) {
	if auth == nil || id == 0 {
		return "", false
	}

	principal, err := auth.PrincipalByID(ctx, id)
	if err != nil {
		return "", false
	}

	return PermissionRevision(principal), true
}
