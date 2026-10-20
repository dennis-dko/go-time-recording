package rest

import (
	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// WeekCopyRequest names the week to copy and the week to copy it into, each by
// its Monday.
type WeekCopyRequest struct {
	From Date `json:"from"`
	To   Date `json:"to"`
}

// WeekCopyResponse is what a copy did: the entries written and their hours, the
// days left as they stood because something was booked on them, and the entries
// left out because their project no longer takes bookings.
type WeekCopyResponse struct {
	Copied int     `json:"copied"`
	Hours  float64 `json:"hours"`
	Kept   int     `json:"kept"`
	Closed int     `json:"closed"`
}

// CopyWeek handles POST /api/v1/me/weeks/copy, which books the caller's entries
// of one week into the days of another that have nothing on them yet.
//
// Under /me/ because it is only ever the caller's own week: there is no user in
// the request, so there is no colleague's week for anybody to name.
func (h *TimesheetHandler) CopyWeek(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermTimesheetWriteOwn)
	if err != nil {
		return nil, err
	}

	var req WeekCopyRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	copied, err := h.timesheets.CopyWeek(c, principal.User.ID, req.From.Time, req.To.Time)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return WeekCopyResponse{
		Copied: copied.Copied, Hours: copied.Hours, Kept: copied.Kept, Closed: copied.Closed,
	}, nil
}
