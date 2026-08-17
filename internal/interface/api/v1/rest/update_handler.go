package rest

import (
	"sync"
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/infrastructure/announce"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/restart"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/selfupdate"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

// UpdateHandler reports whether a newer release exists, and installs it where
// installing is a thing that lasts.
//
// Guarded like the rest of the Settings screen: whoever may configure this
// installation may also update it.
//
// It was narrower - the built-in administrator, or somebody who administers and
// records no time - on the argument that this changes what the application *is*
// rather than what it does, and that is a different kind of decision from
// changing a setting. The argument is sound and it lost to a plainer one: the
// screen is reached by holding settings:manage, everything else on it is
// available to whoever got there, and one card that appears for some of those
// people and not others is a screen that cannot be described in a sentence.
//
// Anybody who could reach this could already grant themselves the narrower right
// anyway, by ticking a permission on a role - so the gate was a step, not a wall.
type UpdateHandler struct {
	authz  *Authorizer
	source *selfupdate.Source

	// hub tells every open browser what is about to happen to the application
	// underneath it. An update is the one thing here that cannot wait for
	// somebody to ask.
	hub *announce.Hub

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

	// failedAt is when the last attempt failed, so the next one waits rather
	// than asking straight into a rate limit that has not cleared.
	failedAt time.Time
}

// NewUpdateHandler creates the handler.
func NewUpdateHandler(authz *Authorizer, source *selfupdate.Source, hub *announce.Hub,
	version string, enabled bool) *UpdateHandler {
	return &UpdateHandler{
		authz: authz, source: source, hub: hub, version: version, enabled: enabled,
	}
}

// How long an answer from the release feed is reused, and how long to wait
// before asking again after a failure.
//
// A minute was far too short, and the cost of that was the failure it produced.
// GitHub allows an unauthenticated caller sixty requests an hour *per address*.
// Every administrator's sign-in starts a check, every open tab repeats it hourly,
// and all of them leave from one address - so an installation with a handful of
// administrators spends its allowance and gets a 403, which reads on screen as a
// permission problem with somebody else's service.
//
// Six hours instead. Releases happen a few times a day at most, the screen polls
// hourly anyway, and one instance now costs four requests a day however many
// people are looking at it. An installation that sets a token gets five thousand
// an hour and never comes near either number.
//
// The wait after a failure is shorter, because a failure is often a network that
// was down for a minute - but long enough that a rate limit is not answered by
// asking again immediately.
const (
	updateCacheFor   = 6 * time.Hour
	updateRetryAfter = 15 * time.Minute
)

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
	if _, err := h.authz.RequireInstallationAdmin(c); err != nil {
		return nil, err
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
	}

	// A version that is known is reported whether or not the last lookup worked.
	// The card says what it knows and, beside it, that the feed is not answering
	// - which is two facts rather than one blank.
	if release.Version == "" {
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

// latest is the newest release, from the feed or from what it last said.
//
// Both are returned when both are known: an answer that is a few hours old is
// worth far more than nothing, and the caller shows the version it knows with
// the trouble noted beside it. Losing the version because the last lookup failed
// meant a rate limit blanked a card that had been right all morning.
func (h *UpdateHandler) latest(c *gofr.Context) (selfupdate.Release, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.hasFreshAnswer() {
		return h.cached, nil
	}

	if h.waitingAfterFailure() {
		return h.cached, h.cacheErr
	}

	release, err := h.source.Latest(c)

	if err != nil {
		h.cacheErr, h.failedAt = err, time.Now()

		// The previous answer is kept rather than thrown away with the attempt.
		return h.cached, err
	}

	h.cached, h.cachedAt, h.cacheErr = release, time.Now(), nil

	return release, nil
}

// hasFreshAnswer reports whether the last answer is recent enough to reuse.
//
// Its own function so the rule can be tested without a clock to wind or a feed
// to stand in for: what is being decided is how old is too old.
func (h *UpdateHandler) hasFreshAnswer() bool {
	return h.cached.Version != "" && time.Since(h.cachedAt) < updateCacheFor
}

// waitingAfterFailure reports whether the last attempt failed recently enough
// that the next one should wait.
//
// Asking again straight away is what turns a rate limit into a rate limit that
// never clears: the allowance is spent, and every retry spends the next one.
func (h *UpdateHandler) waitingAfterFailure() bool {
	return h.cacheErr != nil && time.Since(h.failedAt) < updateRetryAfter
}

// Apply handles POST /api/v1/settings/update.
func (h *UpdateHandler) Apply(c *gofr.Context) (any, error) {
	if _, err := h.authz.RequireInstallationAdmin(c); err != nil {
		return nil, err
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

	// Everybody, before it starts rather than after it finished.
	//
	// The download and its checks take tens of seconds, and on a platform that can
	// replace its own process the restart follows immediately. Whoever pressed the
	// button knows all this; nobody else does, and they are the ones for whom it
	// arrives as the application vanishing mid-sentence. Announced first, so that
	// notice is the length of the download rather than nothing.
	h.hub.Publish(announce.Installing, release.Version)

	if err := h.source.Install(c, release); err != nil {
		// And taken back. The banner promises a restart, and no restart is coming:
		// left standing it would be a permanent lie on everybody's screen.
		h.hub.Publish(announce.Cancelled, release.Version)
		h.hub.Forget()

		return nil, toHTTPError(apperror.Internal(err))
	}

	// Downloaded and in place. Whether it takes effect now or when somebody walks
	// over to the machine is the restart card's question, and the answer differs
	// per platform - so it is reported rather than decided here.
	state := h.describe(c)

	// Which of the two it is, said plainly, because the two mean opposite things
	// to somebody in the middle of typing. A restart that is seconds away is worth
	// stopping for; one that waits for a person to walk to a machine is not worth
	// a banner at all beyond saying so once.
	if state.Restartable {
		h.hub.Publish(announce.Restarting, release.Version)
	} else {
		h.hub.Publish(announce.Pending, release.Version)
	}

	return state, nil
}
