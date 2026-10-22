package rest

import (
	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// NoticesResponse is what about the caller's own time deserves a banner. Each part
// is there only for somebody holding the right to read what it is made of, so an
// account that records no time is answered with nothing rather than refused: the
// screen asks on every load, and a refusal there would be noise in the log.
type NoticesResponse struct {
	Balance *RunningBalanceResponse `json:"balance,omitempty"`
}

// RunningBalanceResponse is the caller's balance from the first day that counts up
// to today, and the bounds they set; see service.RunningBalance.
type RunningBalanceResponse struct {
	Hours   float64  `json:"hours"`
	Since   *Date    `json:"since"`
	Upper   *float64 `json:"upperBound"`
	Lower   *float64 `json:"lowerBound"`
	Outside string   `json:"outside"`
}

// BalanceLimitsRequest sets both bounds of the caller's running balance at once:
// null, or a bound left out, is none.
type BalanceLimitsRequest struct {
	Upper *float64 `json:"upperBound"`
	Lower *float64 `json:"lowerBound"`
}

// Notices handles GET /api/v1/me/notices.
func (h *MeHandler) Notices(c *gofr.Context) (any, error) {
	principal, err := h.authz.Principal(c)
	if err != nil {
		return nil, err
	}

	answer := NoticesResponse{}

	if principal.User.ID == 0 || !principal.Can(model.PermReportReadOwn) {
		return answer, nil
	}

	notices, err := h.overtime.Notices(c, principal.User.ID)
	if err != nil {
		return nil, toHTTPError(err)
	}

	running := notices.Balance
	answer.Balance = &RunningBalanceResponse{
		Hours: running.Hours, Upper: running.Upper, Lower: running.Lower, Outside: running.Outside,
	}

	if running.Since != nil {
		answer.Balance.Since = &Date{Time: *running.Since}
	}

	return answer, nil
}

// SetBalanceLimits handles PUT /api/v1/me/balance-limits.
func (h *MeHandler) SetBalanceLimits(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermSettingsWriteOwn)
	if err != nil {
		return nil, err
	}

	var req BalanceLimitsRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	if err := h.overtime.SetBalanceLimits(c, principal.User.ID, req.Upper, req.Lower); err != nil {
		return nil, toHTTPError(err)
	}

	return h.Notices(c)
}
