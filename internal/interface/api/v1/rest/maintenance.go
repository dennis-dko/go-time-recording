package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// MaintenanceState reports whether the installation is out of service, and can
// be told to forget what it last read.
type MaintenanceState interface {
	State(ctx context.Context) model.Maintenance
	Invalidate()
}

// maintenanceCacheTTL bounds how stale the answer can be.
//
// The middleware runs on every request, and reading a settings row each time
// would put a query in front of every asset. A few seconds of staleness costs
// nothing: switching maintenance on is not a security boundary, it is a notice,
// and the administrator who flipped it can wait two seconds to see it take.
const maintenanceCacheTTL = 2 * time.Second

// cachedMaintenance is a MaintenanceState with a short cache in front of it.
type cachedMaintenance struct {
	read func(ctx context.Context) (model.Maintenance, error)

	mu      sync.RWMutex
	cached  model.Maintenance
	expires time.Time
}

// CachedMaintenanceState wraps a reader with a short cache.
func CachedMaintenanceState(
	read func(ctx context.Context) (model.Maintenance, error),
) *cachedMaintenance {
	return &cachedMaintenance{read: read}
}

func (c *cachedMaintenance) State(ctx context.Context) model.Maintenance {
	c.mu.RLock()
	if time.Now().Before(c.expires) {
		defer c.mu.RUnlock()

		return c.cached
	}
	c.mu.RUnlock()

	state, err := c.read(ctx)
	if err != nil {
		// Unreadable settings must not take the installation down. An
		// unreachable database already fails the request that needs it.
		return model.Maintenance{}
	}

	c.mu.Lock()
	c.cached, c.expires = state, time.Now().Add(maintenanceCacheTTL)
	c.mu.Unlock()

	return state
}

// Invalidate drops the cached answer.
//
// Called when the switch is flipped, so it takes effect on the next request
// rather than within the cache interval. An administrator who turns maintenance
// on and watches somebody book time for another two seconds has been told a lie
// by their own interface - and the same interval on the way out means work stays
// refused after they said it should not be.
func (c *cachedMaintenance) Invalidate() {
	c.mu.Lock()
	c.expires = time.Time{}
	c.mu.Unlock()
}

// MaintenanceMiddleware turns requests away while the installation is out of
// service.
//
// # What still works, and why each one has to
//
// The interface itself, so somebody who opens the page sees the notice rather
// than a bare status code from the browser. Signing in and signing out, and the
// endpoints the administration screen needs, because the only way out of
// maintenance mode is through that screen - a mode that locks out the person who
// has to end it is a trap, not a feature.
//
// The health endpoint, which is not under /api/ and so is covered by the same
// rule as the interface. That matters: an orchestrator that concludes the
// container is dead replaces it, and the replacement comes up in maintenance mode
// too. Metrics are on their own port and never reach this middleware at all.
//
// # Who still works
//
// Whoever may administer this installation: the built-in account, and anybody
// holding model.PermSettingsManage. Everyone else is turned away, because
// "nobody is writing to this database right now" is the whole point, and every
// exception is a way for that to be untrue.
//
// The exemption used to be the built-in account and nothing else, and that made
// maintenance mode a reason to sign in as it - the one account whose actions are
// hardest to attribute to a person, used for exactly the work where you most want
// to know who did it. The people fixing an installation are the people who may
// configure it, so it is the same right the Settings screen is gated on rather
// than a second answer to "who may administer".
//
// # The status
//
// 503 with Retry-After. A monitor reads that as "down on purpose, come back",
// where a 200 with an apology in the body reads as working and a 500 reads as
// broken.
func MaintenanceMiddleware(state MaintenanceState) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if state == nil {
				next.ServeHTTP(w, r)

				return
			}

			maintenance := state.State(r.Context())
			if !maintenance.Enabled {
				next.ServeHTTP(w, r)

				return
			}

			if maintenanceExempt(r) || isInstallationAdminRequest(r) {
				next.ServeHTTP(w, r)

				return
			}

			respondUnavailable(w, maintenance)
		})
	}
}

// maintenanceExempt reports whether a path has to keep working regardless.
func maintenanceExempt(r *http.Request) bool {
	path := r.URL.Path

	// Everything the interface is made of. Without these the page cannot render
	// the notice, and the browser shows its own error instead.
	if !strings.HasPrefix(path, "/api/") {
		return true
	}

	const base = "/api/v1"

	// Sign-in and sign-out: the administrator has to be able to get in, and
	// anybody already in should be able to leave cleanly.
	switch path {
	case base + "/auth/login", base + "/auth/logout", base + "/branding", base + "/languages":
		return true
	case base + "/auth/passkey", base + "/auth/passkey/login":
		return true
	case base + "/me", base + "/maintenance":
		// /me so the interface knows who it is talking to and can decide what to
		// show; /maintenance so it can read and clear the state.
		return true
	}

	// The administration screen, which is where maintenance mode is turned off.
	// Reads and writes both: an administrator who can see the switch but not
	// flip it is no better off.
	return strings.HasPrefix(path, base+"/settings/") || strings.HasPrefix(path, base+"/admin/")
}

// isInstallationAdminRequest reports whether the caller may configure this
// installation, which is what Authorizer.RequireInstallationAdmin asks: the
// built-in account, or somebody whose role holds model.PermSettingsManage.
//
// The rule is spelled a second time rather than delegated, because this
// middleware runs in front of the router and has an *http.Request where the
// authorizer needs a gofr.Context. What it does share is where the answer comes
// from: the principal SessionMiddleware resolved from the cookie and left on the
// request context, which is the same principal the authorizer would read. A
// request that arrives without a session has none, and holds nothing.
func isInstallationAdminRequest(r *http.Request) bool {
	principal, ok := principalFromContext(r.Context())
	if !ok || principal == nil || principal.User == nil {
		return false
	}

	return principal.User.IsSystem || principal.Can(model.PermSettingsManage)
}

// maintenanceRetryAfter is what a caller is told to wait. Deliberately short:
// this is a notice to come back, not a schedule anybody can promise.
const maintenanceRetryAfter = 120

func respondUnavailable(w http.ResponseWriter, maintenance model.Maintenance) {
	w.Header().Set("Retry-After", strconv.Itoa(maintenanceRetryAfter))
	w.Header().Set("Cache-Control", "no-store")

	// The same shape every other error in this API uses, so a client that
	// already reads error.message needs no special case for this one.
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)

	body := map[string]any{
		"message":     maintenance.Text(),
		"maintenance": true,
		"retryAfter":  maintenanceRetryAfter,
	}

	// A code only where the text is this application's own. An administrator who
	// wrote the notice wrote it for the people who will read it, so translating it
	// would replace their words with ours; the default is ours, and a German reader
	// met it in English.
	if maintenance.Message == "" {
		body["code"] = "maintenance"
	}

	_ = json.NewEncoder(w).Encode(map[string]any{"error": body})
}
