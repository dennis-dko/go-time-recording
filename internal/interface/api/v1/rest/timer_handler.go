package rest

import (
	"strings"
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
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

// TimerAdjustment corrects when the running clock started.
type TimerAdjustment struct {
	// StartedAt is a moment as parseMoment reads it.
	StartedAt string `json:"startedAt"`
}

// TimerStopRequest is what stopping may say: when the work ended, or nothing,
// which means now.
type TimerStopRequest struct {
	// EndedAt is a moment as parseMoment reads it.
	EndedAt string `json:"endedAt"`
}

// TimerSwitchResponse is a switch's outcome: the entry it booked, and the clock
// that has been running since the same second.
type TimerSwitchResponse struct {
	Booked TimesheetResponse `json:"booked"`
	Timer  TimerResponse     `json:"timer"`
}

// Adjust handles PUT /api/v1/me/timer, which corrects when the clock started.
func (h *TimerHandler) Adjust(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermTimesheetWriteOwn)
	if err != nil {
		return nil, err
	}

	var req TimerAdjustment
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	startedAt, ok := parseMoment(req.StartedAt, principal.User.TimezoneOf(h.timezone.resolve(c)))
	if !ok {
		return nil, toHTTPError(apperror.InvalidFields("startedAt"))
	}

	timer, err := h.timers.AdjustStart(c, principal.User.ID, startedAt)
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
	// the evening books on the evening it was started - and it is the clock an
	// end time without an offset was read from.
	zone := principal.User.TimezoneOf(h.timezone.resolve(c))

	// No body is the ordinary stop, now. A body that is there has to say
	// something readable: stopping at the current moment because a written end
	// time could not be read would book a different duration from the one asked
	// for, with nothing to show it happened.
	var endedAt *time.Time

	if raw := requestOf(c); raw != nil && raw.ContentLength != 0 {
		var req TimerStopRequest
		if err := bind(c, &req); err != nil {
			return nil, toHTTPError(err)
		}

		if strings.TrimSpace(req.EndedAt) != "" {
			ended, ok := parseMoment(req.EndedAt, zone)
			if !ok {
				return nil, toHTTPError(apperror.InvalidFields("endedAt"))
			}

			endedAt = &ended
		}
	}

	created, err := h.timers.Stop(c, principal.User.ID, zone, endedAt)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return newTimesheetResponse(created.Result), nil
}

// Switch handles POST /api/v1/me/timer/switch: the running clock is booked and the
// next one starts at the same second.
func (h *TimerHandler) Switch(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermTimesheetWriteOwn)
	if err != nil {
		return nil, err
	}

	// Empty is a fine answer here too: carry on without a project, and say what
	// it was later.
	var req TimerRequest
	_ = bind(c, &req)

	zone := principal.User.TimezoneOf(h.timezone.resolve(c))

	switched, err := h.timers.Switch(c, principal.User.ID, zone, req.ProjectID, req.Description)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return TimerSwitchResponse{
		Booked: newTimesheetResponse(switched.Booked.Result),
		Timer:  newTimerResponse(switched.Timer),
	}, nil
}

// parseMoment reads a moment that a person typed or a program sent.
//
// RFC 3339 with its offset is unambiguous and taken as it is. Without an offset it
// is a time on a wall clock, read in zone - the caller's own - because that is the
// clock the person typing it was looking at, and a browser cannot turn a time in an
// arbitrary named zone into an instant by itself.
func parseMoment(raw string, zone *time.Location) (time.Time, bool) {
	raw = strings.TrimSpace(raw)

	if moment, err := time.Parse(time.RFC3339, raw); err == nil {
		return moment, true
	}

	if zone == nil {
		zone = time.UTC
	}

	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04"} {
		if moment, err := time.ParseInLocation(layout, raw, zone); err == nil {
			return moment, true
		}
	}

	return time.Time{}, false
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
