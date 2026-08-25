package rest

import (
	"errors"
	"sync"
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/infrastructure/announce"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/imageupdate"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/restart"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/selfupdate"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
	"github.com/dennis-dko/go-time-recording/internal/pkg/hosting"
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

	// images asks the container that holds the Docker socket to pull a new image
	// and recreate this container from it. Nil, or reporting itself unavailable,
	// on every deployment that has not added that overlay - which is the default.
	images *imageupdate.Updater
}

// WithImageUpdater lets a container deployment update by the image rather than
// by the binary.
//
// The two are different updates and only one of them lasts. Swapping the binary
// changes this container and not the image it was made from, so the next
// recreate brings the old version back; pulling the image changes what every
// future container will be. Where both are possible the image wins, because the
// other one is a repair that expires.
func (h *UpdateHandler) WithImageUpdater(images *imageupdate.Updater) *UpdateHandler {
	h.images = images

	return h
}

// byImage reports whether this installation updates by pulling an image.
func (h *UpdateHandler) byImage() bool {
	return h.images != nil && h.images.Available()
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

	// Installable says the button can do something that lasts.
	//
	// False in a container with no updater beside it. Swapping the binary there
	// works and does not last: it changes this container and not the image it
	// was made from, so the next recreate brings the old version back - and a
	// recreate is what a container deployment does to apply anything. Offering
	// it would be offering an update that reverts on a day nobody connects to
	// the button they pressed.
	//
	// True in a container that has one, because then the image is what changes.
	// See ByImage, and deploy/compose.update.yaml.
	Installable bool   `json:"installable"`
	Why         string `json:"why,omitempty"`

	// ByImage says this installation updates by pulling an image and recreating
	// its container, rather than by swapping the binary inside it. True only
	// where the deployment has added the updater that holds the Docker socket;
	// see deploy/compose.update.yaml.
	//
	// It changes what the button promises, so the screen is told rather than
	// left to guess from the caveat.
	ByImage bool `json:"byImage,omitempty"`

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
	byImage := h.byImage()

	out := UpdateResponse{
		Running:     h.version,
		Enabled:     h.enabled,
		Installable: byImage || !hosting.InContainer(),
		ByImage:     byImage,
		Restartable: restart.Supported(),
		RestartCode: restart.Code(),
		Comparable:  selfupdate.Comparable(h.version),
	}

	// A container with nothing that can replace its image updates the way every
	// container deployment does, by hand. The screen says which command.
	if !out.Installable {
		out.Why = "inContainer"
	}

	if pending, ok := selfupdate.Installed(); ok {
		out.Pending = pending
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

// afterAFailedInstall decides what a refused install leaves standing, and says
// what happened.
//
// Two answers, and the difference between them is the banner. Apply raises one
// before the download starts, because the download is the notice period: it
// warns everybody looking at a screen that the application is about to go away
// under them.
//
// A genuine failure means no restart is coming, so that warning has to come down
// - left up it is a permanent lie on everybody's screen.
//
// An install already under way means the opposite. The restart is still coming,
// raised by the call that got there first, and the banner belongs to that call.
// Two administrators pressing within a moment of each other, or one pressing
// twice, is the ordinary way in - and taking the warning down because the second
// press was turned away would leave everybody unwarned about an update that is
// still running. So this is answered before the hub is touched at all.
func (h *UpdateHandler) afterAFailedInstall(err error, version string) error {
	if errors.Is(err, selfupdate.ErrInstalling) {
		return toHTTPError(apperror.Conflictf(
			"an update is already being installed").WithCode("updateInstalling"))
	}

	h.hub.Publish(announce.Cancelled, version)
	h.hub.Forget()

	return toHTTPError(apperror.Internal(err))
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

	release, err := h.latest(c)
	if err != nil {
		return nil, toHTTPError(apperror.Internal(err))
	}

	if !selfupdate.Newer(h.version, release.Version) {
		return nil, toHTTPError(apperror.Invalidf("this installation already runs %s",
			h.version).WithCode("updateNotNewer", h.version))
	}

	// A container with an updater beside it takes the image instead.
	//
	// Not the binary as well: the two are different updates and only one of them
	// lasts. Swapping the binary changes this container and not the image it was
	// made from, so it is a repair that expires at the next recreate - and the
	// recreate is exactly what the updater is about to perform. Doing both would
	// download thirty megabytes to throw them away a second later.
	if h.byImage() {
		return h.askForTheImage(c, release.Version)
	}

	// And a container without one is not offered this at all. Checked here
	// rather than trusted from the screen: the button is only drawn where this
	// holds, and a POST is not a button.
	if hosting.InContainer() {
		return nil, toHTTPError(apperror.Invalidf("this runs in a container, where " +
			"replacing the binary is undone by the next recreate; update the image " +
			"instead, or add deploy/compose.update.yaml to do it from here").
			WithCode("updateInContainer"))
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
		return nil, h.afterAFailedInstall(err, release.Version)
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

// askForTheImage hands the job to the container that can do it.
//
// Nothing is downloaded here and nothing waits for the answer. The updater
// pulls, recreates this container, and this process stops existing part way
// through the reply - which is why the announcement goes out first, and why the
// browser learns it worked by watching the version come back different rather
// than by reading a response.
//
// Announced as a restart rather than as an install, because that is what it
// looks like from every screen: the application goes away and comes back. The
// difference between the two - which is whether the new version survives the
// next recreate - matters to the person who pressed the button and to nobody
// else in the building.
func (h *UpdateHandler) askForTheImage(c *gofr.Context, version string) (any, error) {
	h.hub.Publish(announce.Installing, version)

	if err := h.images.Ask(); err != nil {
		h.hub.Publish(announce.Cancelled, version)
		h.hub.Forget()

		if errors.Is(err, imageupdate.ErrBusy) {
			return nil, toHTTPError(apperror.Conflictf(
				"an update is already running").WithCode("updateInstalling"))
		}

		c.Logger.Errorf("could not ask for a new image: %v", err)

		return nil, toHTTPError(apperror.Internal(err))
	}

	h.hub.Publish(announce.Restarting, version)

	return h.describe(c), nil
}
