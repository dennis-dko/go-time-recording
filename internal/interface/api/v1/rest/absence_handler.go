package rest

import (
	"strings"
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// AbsenceHandler lets somebody record the days they are away, and why.
//
// Under /me/ and behind the rights to one's own time: an absence is a record about
// one's own days, read and written as entries are, and nobody records one for
// anybody else - so there is no user id in these routes for anybody to get wrong.
type AbsenceHandler struct {
	absences *service.AbsenceService
	authz    *Authorizer
	timezone InstanceTimezoneFunc
}

// NewAbsenceHandler creates the handler.
func NewAbsenceHandler(absences *service.AbsenceService, authz *Authorizer, timezone InstanceTimezoneFunc) *AbsenceHandler {
	return &AbsenceHandler{absences: absences, authz: authz, timezone: timezone}
}

// AbsenceResponse is one day away, and why.
type AbsenceResponse struct {
	Day  Date   `json:"day"`
	Kind string `json:"kind"`
}

// AbsenceRequest records the working days of a range as away, for one reason.
type AbsenceRequest struct {
	From Date   `json:"from"`
	To   Date   `json:"to"`
	Kind string `json:"kind"`
}

func absenceItems(absences []model.Absence) listResponse[AbsenceResponse] {
	items := make([]AbsenceResponse, 0, len(absences))
	for _, absence := range absences {
		items = append(items, AbsenceResponse{Day: Date{Time: absence.Day}, Kind: absence.Kind})
	}

	return listResponse[AbsenceResponse]{Items: items, TotalCount: uint(len(items))}
}

// List handles GET /api/v1/me/absences, the caller's absences over a range -
// by default the calendar year today falls in, in their own zone.
func (h *AbsenceHandler) List(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermTimesheetReadOwn)
	if err != nil {
		return nil, err
	}

	from, to, err := yearRange(c, principal.User.TimezoneOf(h.timezone.resolve(c)))
	if err != nil {
		return nil, toHTTPError(err)
	}

	absences, err := h.absences.Absences(c, principal.User.ID, from, to)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return absenceItems(absences), nil
}

// Save handles POST /api/v1/me/absences, which records the working days of a range
// as away and answers with the days it recorded.
func (h *AbsenceHandler) Save(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermTimesheetWriteOwn)
	if err != nil {
		return nil, err
	}

	var req AbsenceRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	recorded, err := h.absences.Record(c, principal.User.ID, req.From.Time, req.To.Time, req.Kind)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return absenceItems(recorded), nil
}

// Delete handles DELETE /api/v1/me/absences/{day}.
func (h *AbsenceHandler) Delete(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermTimesheetWriteOwn)
	if err != nil {
		return nil, err
	}

	day, err := time.Parse(time.DateOnly, strings.TrimSpace(c.PathParam("day")))
	if err != nil {
		return nil, toHTTPError(apperror.InvalidFields("day"))
	}

	if err := h.absences.Remove(c, principal.User.ID, day); err != nil {
		return nil, toHTTPError(err)
	}

	return map[string]string{"status": "deleted"}, nil
}

// yearRange reads the from/to parameters, defaulting to the calendar year today
// falls in - today in the caller's own zone, for the reason weekRange gives.
func yearRange(c *gofr.Context, location *time.Location) (from, to time.Time, err error) {
	fromParam, err := queryDate(c, "from")
	if err != nil {
		return from, to, err
	}

	toParam, err := queryDate(c, "to")
	if err != nil {
		return from, to, err
	}

	year := time.Now().In(location).Year()

	from = time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC)
	to = time.Date(year, time.December, 31, 0, 0, 0, 0, time.UTC)

	if fromParam != nil {
		from = *fromParam
	}

	if toParam != nil {
		to = *toParam
	}

	return from, to, nil
}
