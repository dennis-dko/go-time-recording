package rest

import (
	"strings"
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// ScheduleHandler lets somebody plan the weeks their hours are measured against.
//
// Under /me/ like the clock, and behind the right that writes the daily target: a
// plan is a time figure about the person it belongs to, and nobody plans anybody
// else's week - so there is no user id in these routes for anybody to get wrong.
type ScheduleHandler struct {
	weeks *service.WorkScheduleService
	authz *Authorizer
}

// NewScheduleHandler creates the handler.
func NewScheduleHandler(weeks *service.WorkScheduleService, authz *Authorizer) *ScheduleHandler {
	return &ScheduleHandler{weeks: weeks, authz: authz}
}

// ScheduleResponse is one planned week.
type ScheduleResponse struct {
	ValidFrom Date `json:"validFrom"`

	// Hours are the seven weekdays, Monday first.
	Hours model.WeekHours `json:"hours"`
}

// SchedulesResponse is somebody's plans, earliest first, and the week that
// applies before the first of them - their daily target from Monday to Friday.
type SchedulesResponse struct {
	Items  []ScheduleResponse `json:"items"`
	Before model.WeekHours    `json:"before"`
}

// ScheduleRequest plans a week from a day on.
type ScheduleRequest struct {
	ValidFrom Date `json:"validFrom"`

	// Hours is a slice rather than an array so that six numbers, or eight, are
	// refused: decoded into an array they would quietly become a week with a day
	// missing or one dropped.
	Hours []float64 `json:"hours"`
}

func newScheduleResponse(schedule model.WorkSchedule) ScheduleResponse {
	return ScheduleResponse{ValidFrom: Date{schedule.ValidFrom}, Hours: schedule.Hours}
}

// List handles GET /api/v1/me/schedules.
func (h *ScheduleHandler) List(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermSettingsWriteOwn)
	if err != nil {
		return nil, err
	}

	planned, err := h.weeks.Planned(c, principal.User.ID)
	if err != nil {
		return nil, toHTTPError(err)
	}

	items := make([]ScheduleResponse, 0, len(planned.Schedules))
	for _, schedule := range planned.Schedules {
		items = append(items, newScheduleResponse(schedule))
	}

	return SchedulesResponse{Items: items, Before: planned.Before}, nil
}

// Save handles POST /api/v1/me/schedules, which plans a week from a day on and
// replaces a plan starting that same day.
func (h *ScheduleHandler) Save(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermSettingsWriteOwn)
	if err != nil {
		return nil, err
	}

	var req ScheduleRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	if len(req.Hours) != len(model.WeekHours{}) {
		return nil, toHTTPError(apperror.InvalidFields("hours"))
	}

	var hours model.WeekHours

	copy(hours[:], req.Hours)

	schedule, err := h.weeks.Plan(c, principal.User.ID, req.ValidFrom.Time, hours)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return newScheduleResponse(*schedule), nil
}

// Delete handles DELETE /api/v1/me/schedules/{validFrom}.
func (h *ScheduleHandler) Delete(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermSettingsWriteOwn)
	if err != nil {
		return nil, err
	}

	validFrom, err := time.Parse(time.DateOnly, strings.TrimSpace(c.PathParam("validFrom")))
	if err != nil {
		return nil, toHTTPError(apperror.InvalidFields("validFrom"))
	}

	if err := h.weeks.Remove(c, principal.User.ID, validFrom); err != nil {
		return nil, toHTTPError(err)
	}

	return map[string]string{"status": "removed"}, nil
}
