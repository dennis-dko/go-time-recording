package rest

import (
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// TimerHandler starts and stops the clock for the caller's own day.
//
// Under /me/, and only ever for the caller: starting somebody else's clock would
// record time they were not asked about, and there is no reason anybody would want
// to. So there is no user id anywhere in these routes, and no permission for
// "other people's timers" to get wrong.
type TimerHandler struct {
	timers   *service.TimerService
	authz    *Authorizer
	timezone InstanceTimezoneFunc
}

// NewTimerHandler creates the handler.
func NewTimerHandler(
	timers *service.TimerService,
	authz *Authorizer,
	timezone InstanceTimezoneFunc,
) *TimerHandler {
	return &TimerHandler{timers: timers, authz: authz, timezone: timezone}
}

// TimerRequest is what starting a clock may say about it.
type TimerRequest struct {
	// ProjectID is optional, as it is on a booking: time can be recorded first
	// and categorised afterwards.
	ProjectID   *uint   `json:"projectId"`
	Description *string `json:"description"`
}

// TimerResponse is the running clock, or the absence of one.
type TimerResponse struct {
	// Running is false when nothing is going, and then nothing else is set.
	Running bool `json:"running"`

	ProjectID   *uint   `json:"projectId,omitempty"`
	Description *string `json:"description,omitempty"`

	// StartedAt is when it started, so the interface can count up on its own
	// rather than polling to be told the same thing every second.
	StartedAt *time.Time `json:"startedAt,omitempty"`

	// ElapsedHours is what has accumulated so far, in the unit a booking uses.
	// Sent as well as StartedAt so a browser with a wrong clock still shows the
	// duration the server would record.
	ElapsedHours float64 `json:"elapsedHours"`
}

func newTimerResponse(timer *model.RunningTimer) TimerResponse {
	if timer == nil {
		return TimerResponse{Running: false}
	}

	startedAt := timer.StartedAt

	return TimerResponse{
		Running:      true,
		ProjectID:    timer.ProjectID,
		Description:  timer.Description,
		StartedAt:    &startedAt,
		ElapsedHours: timer.HoursElapsed(time.Now().UTC()),
	}
}

// Running handles GET /api/v1/me/timer.
func (h *TimerHandler) Running(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermTimesheetWriteOwn)
	if err != nil {
		return nil, err
	}

	timer, err := h.timers.Running(c, principal.User.ID)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return newTimerResponse(timer), nil
}

// Start handles POST /api/v1/me/timer.
func (h *TimerHandler) Start(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermTimesheetWriteOwn)
	if err != nil {
		return nil, err
	}

	// An empty body is a perfectly good "start timing, I will say what it was
	// later", so a payload that will not bind is not fatal here.
	var req TimerRequest
	_ = bind(c, &req)

	timer, err := h.timers.Start(c, principal.User.ID, req.ProjectID, req.Description)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return newTimerResponse(timer), nil
}

// Stop handles POST /api/v1/me/timer/stop, and answers with the entry it created.
func (h *TimerHandler) Stop(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermTimesheetWriteOwn)
	if err != nil {
		return nil, err
	}

	// The caller's own zone decides the calendar day, so a clock started late in
	// the evening books on the evening it was started.
	zone := principal.User.TimezoneOf(h.timezone.resolve(c))

	created, err := h.timers.Stop(c, principal.User.ID, zone)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return newTimesheetResponse(created.Result), nil
}

// Discard handles DELETE /api/v1/me/timer.
func (h *TimerHandler) Discard(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermTimesheetWriteOwn)
	if err != nil {
		return nil, err
	}

	if err := h.timers.Discard(c, principal.User.ID); err != nil {
		return nil, toHTTPError(err)
	}

	return TimerResponse{Running: false}, nil
}
