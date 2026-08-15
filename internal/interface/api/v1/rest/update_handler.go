package rest

import (
	"sync"
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/infrastructure/restart"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/selfupdate"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

// UpdateHandler reports whether a newer release exists, and installs it where
// installing is a thing that lasts.
//
// Guarded by AdministersOnly rather than by settings:manage, and it is the only
// screen here that is. Everything else on Settings changes what the application
// does; this one changes what the application *is* - the bytes that will be
// executed after the next start. That is the built-in administrator's decision,
// or that of somebody the installation deliberately made equivalent to it.
type UpdateHandler struct {
	authz  *Authorizer
	source *selfupdate.Source

	// version is what this process was built as. "dev" for anything not built
	// from a tag, which is why the answer below says whether it could compare at
	// all rather than only whether something is newer.
	version string

	// enabled is false where an installation has asked not to call out. An
	// air-gapped deployment should not be reaching for a release feed on every
	// visit to the administration screen.
	enabled bool

	// The last answer, and when. The screen asks on every load and the feed is
	// somebody else's service; a check a minute is plenty for something released
	// a few times a day at most.
	mu       sync.Mutex
	cached   selfupdate.Release
	cachedAt time.Time
	cacheErr error
}

// NewUpdateHandler creates the handler.
func NewUpdateHandler(authz *Authorizer, source *selfupdate.Source, version string,
	enabled bool) *UpdateHandler {
	return &UpdateHandler{authz: authz, source: source, version: version, enabled: enabled}
}

// updateCacheFor is how long an answer from the release feed is reused.
const updateCacheFor = time.Minute

// UpdateResponse is what the screen needs to say something true on every
// platform.
type UpdateResponse struct {
	// Running is this build, and Latest is what the feed said. Latest is empty
	// when the check is off or could not be made.
	Running string `json:"running"`
	Latest  string `json:"latest,omitempty"`

	// Available is the only field the button should key on: a newer version
	// exists, published something for this platform, and this deployment is one
	// that can install it.
	Available bool `json:"available"`

	// Newer says a later version exists whether or not it can be installed from
	// here, which is what a container needs to be told.
	Newer bool `json:"newer"`

	// Installable is false in a container, where a swapped binary is undone by
	// the next recreate. The screen says what to run instead.
	Installable bool   `json:"installable"`
	Why         string `json:"why,omitempty"`

	// Pending is the version already downloaded and waiting for a restart, so the
	// same update is not offered twice to somebody who has taken it.
	Pending string `json:"pending,omitempty"`

	// Restartable says whether this process can replace itself, so the screen can
	// promise a restart or ask for one.
	Restartable bool   `json:"restartable"`
	RestartCode string `json:"restartCode,omitempty"`

	// Notes and URL are the release's own words, so nobody has to take an
	// application's word for what it is about to install.
	Notes string `json:"notes,omitempty"`
	URL   string `json:"url,omitempty"`

	// Enabled is false where the installation asked not to check at all.
	Enabled bool `json:"enabled"`

	// Comparable is false for a build not made from a tag, which calls itself
	// "dev". Without it the screen would report "dev is the newest version" -
	// untrue, and the sort of untrue that stops somebody looking further.
	Comparable bool `json:"comparable"`

	// Problem carries a failed lookup as information rather than as an error:
	// GitHub being unreachable is not this application being broken, and it must
	// not colour the whole screen red.
	Problem string `json:"problem,omitempty"`
}

// State handles GET /api/v1/settings/update.
func (h *UpdateHandler) State(c *gofr.Context) (any, error) {
	principal, err := h.authz.Principal(c)
	if err != nil {
		return nil, err
	}

	if !h.authz.AdministersOnly(principal) {
		return nil, forbiddenError{msg: "only an administrator of this installation may " +
			"update it"}.WithCode("onlyBuiltInAdminUpdates")
	}

	return h.describe(c), nil
}

// describe builds the answer, asking the feed at most once a minute.
func (h *UpdateHandler) describe(c *gofr.Context) UpdateResponse {
	out := UpdateResponse{
		Running:     h.version,
		Enabled:     h.enabled,
		Installable: !selfupdate.InContainer(),
		Restartable: restart.Supported(),
		RestartCode: restart.Code(),
		Comparable:  selfupdate.Comparable(h.version),
	}

	if pending, ok := selfupdate.Installed(); ok {
		out.Pending = pending
	}

	if !out.Installable {
		out.Why = "inContainer"
	}

	if !h.enabled {
		return out
	}

	release, err := h.latest(c)
	if err != nil {
		out.Problem = err.Error()

		return out
	}

	out.Latest = release.Version
	out.Notes = release.Notes
	out.URL = release.URL
	out.Newer = selfupdate.Newer(h.version, release.Version)

	// Everything has to be true at once: something newer, something published for
	// this platform, somewhere it will still be there tomorrow, and not already
	// downloaded.
	out.Available = out.Newer && release.HasBinary() && out.Installable &&
		out.Pending != release.Version

	return out
}

func (h *UpdateHandler) latest(c *gofr.Context) (selfupdate.Release, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if time.Since(h.cachedAt) < updateCacheFor && (h.cached.Version != "" || h.cacheErr != nil) {
		return h.cached, h.cacheErr
	}

	release, err := h.source.Latest(c)

	h.cached, h.cacheErr, h.cachedAt = release, err, time.Now()

	return release, err
}

// Apply handles POST /api/v1/settings/update.
func (h *UpdateHandler) Apply(c *gofr.Context) (any, error) {
	principal, err := h.authz.Principal(c)
	if err != nil {
		return nil, err
	}

	if !h.authz.AdministersOnly(principal) {
		return nil, forbiddenError{msg: "only an administrator of this installation may " +
			"update it"}.WithCode("onlyBuiltInAdminUpdates")
	}

	if !h.enabled {
		return nil, toHTTPError(apperror.Invalidf("updating is switched off on this " +
			"installation").WithCode("updateDisabled"))
	}

	// Checked again here rather than trusted from the screen. The button is only
	// offered where this holds, and a POST is not a button.
	if selfupdate.InContainer() {
		return nil, toHTTPError(apperror.Invalidf("this runs in a container, where " +
			"replacing the binary is undone by the next recreate").
			WithCode("updateInContainer"))
	}

	release, err := h.latest(c)
	if err != nil {
		return nil, toHTTPError(apperror.Internal(err))
	}

	if !selfupdate.Newer(h.version, release.Version) {
		return nil, toHTTPError(apperror.Invalidf("this installation already runs %s",
			h.version).WithCode("updateNotNewer", h.version))
	}

	if err := h.source.Install(c, release); err != nil {
		return nil, toHTTPError(apperror.Internal(err))
	}

	// Downloaded and in place. Whether it takes effect now or when somebody walks
	// over to the machine is the restart card's question, and the answer differs
	// per platform - so it is reported rather than decided here.
	return h.describe(c), nil
}
