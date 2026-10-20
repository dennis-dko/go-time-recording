package rest

import (
	"strconv"
	"strings"
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// VacationHandler lets somebody keep how many days of leave a year gives them, and
// see what is left of it.
//
// Under /me/ like the absences it is counted from: leave is a figure about the
// person it belongs to, read with the right to read one's own time and set with the
// right that sets one's own daily target.
type VacationHandler struct {
	leave    *service.VacationService
	authz    *Authorizer
	timezone InstanceTimezoneFunc
}

// NewVacationHandler creates the handler.
func NewVacationHandler(leave *service.VacationService, authz *Authorizer, timezone InstanceTimezoneFunc) *VacationHandler {
	return &VacationHandler{leave: leave, authz: authz, timezone: timezone}
}

// VacationEntitlementResponse is how many days of leave a year gives, from a year on.
type VacationEntitlementResponse struct {
	FromYear int     `json:"fromYear"`
	Days     float64 `json:"days"`
}

// VacationResponse is a year of somebody's leave.
type VacationResponse struct {
	Year        int     `json:"year"`
	Entitlement float64 `json:"entitlement"`
	CarriedOver float64 `json:"carriedOver"`
	Taken       float64 `json:"taken"`
	Planned     float64 `json:"planned"`
	Remaining   float64 `json:"remaining"`

	Entitlements []VacationEntitlementResponse `json:"entitlements"`
}

// VacationRequest gives days of leave a year from a year on.
type VacationRequest struct {
	FromYear int `json:"fromYear"`

	// Days is a pointer so that leaving it out is refused rather than read as
	// a year that gives none.
	Days *float64 `json:"days"`
}

func newVacationResponse(account *service.VacationAccount) VacationResponse {
	answer := VacationResponse{
		Year:         account.Year,
		Entitlement:  account.Entitlement,
		CarriedOver:  account.CarriedOver,
		Taken:        account.Taken,
		Planned:      account.Planned,
		Remaining:    account.Remaining(),
		Entitlements: make([]VacationEntitlementResponse, 0, len(account.Entitlements)),
	}

	for _, entitlement := range account.Entitlements {
		answer.Entitlements = append(answer.Entitlements,
			VacationEntitlementResponse{FromYear: entitlement.FromYear, Days: entitlement.Days})
	}

	return answer
}

// Year handles GET /api/v1/me/vacation, a year of the caller's leave - by default
// the year today falls in, in their own zone.
func (h *VacationHandler) Year(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermTimesheetReadOwn)
	if err != nil {
		return nil, err
	}

	year := time.Now().In(principal.User.TimezoneOf(h.timezone.resolve(c))).Year()

	if raw := strings.TrimSpace(c.Param("year")); raw != "" {
		year, err = strconv.Atoi(raw)
		if err != nil {
			return nil, toHTTPError(apperror.InvalidFields("year"))
		}
	}

	account, err := h.leave.Year(c, principal.User.ID, year)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return newVacationResponse(account), nil
}

// Entitle handles PUT /api/v1/me/vacation, which gives days of leave a year from a
// year on and answers with that year's account.
func (h *VacationHandler) Entitle(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermSettingsWriteOwn)
	if err != nil {
		return nil, err
	}

	var req VacationRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	if req.Days == nil {
		return nil, toHTTPError(apperror.InvalidFields("days"))
	}

	if err := h.leave.Entitle(c, principal.User.ID, req.FromYear, *req.Days); err != nil {
		return nil, toHTTPError(err)
	}

	account, err := h.leave.Year(c, principal.User.ID, req.FromYear)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return newVacationResponse(account), nil
}

// Withdraw handles DELETE /api/v1/me/vacation/{fromYear}.
func (h *VacationHandler) Withdraw(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermSettingsWriteOwn)
	if err != nil {
		return nil, err
	}

	fromYear, err := strconv.Atoi(strings.TrimSpace(c.PathParam("fromYear")))
	if err != nil {
		return nil, toHTTPError(apperror.InvalidFields("fromYear"))
	}

	if err := h.leave.Withdraw(c, principal.User.ID, fromYear); err != nil {
		return nil, toHTTPError(err)
	}

	return map[string]string{"status": "deleted"}, nil
}
