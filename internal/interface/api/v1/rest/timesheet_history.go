package rest

import (
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// TimesheetRevisionResponse is one earlier version of an entry, and when a
// correction replaced it.
type TimesheetRevisionResponse struct {
	Date          Date      `json:"date"`
	DurationHours float64   `json:"durationHours"`
	ProjectID     *uint     `json:"projectId"`
	Description   *string   `json:"description"`
	ReplacedAt    time.Time `json:"replacedAt"`
}

// Revisions handles GET /api/v1/timesheets/{id}/revisions: what an entry said
// before each correction, the most recently replaced first.
//
// For its owner only. Anybody else is told the entry is not there, which is what
// they are told about the entry itself.
func (h *TimesheetHandler) Revisions(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermTimesheetReadOwn)
	if err != nil {
		return nil, err
	}

	id, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	history, err := h.timesheets.Revisions(c, id)
	if err != nil {
		return nil, toHTTPError(err)
	}

	if err := h.authz.requireOwnEntry(principal, id, history.OwnerID); err != nil {
		return nil, err
	}

	items := make([]TimesheetRevisionResponse, 0, len(history.Revisions))

	for _, revision := range history.Revisions {
		items = append(items, TimesheetRevisionResponse{
			Date: Date{Time: model.CalendarDay(revision.Date)}, DurationHours: revision.DurationHours,
			ProjectID: revision.ProjectID, Description: revision.Description,
			ReplacedAt: revision.ReplacedAt.UTC(),
		})
	}

	return listResponse[TimesheetRevisionResponse]{Items: items, TotalCount: uint(len(items))}, nil
}
