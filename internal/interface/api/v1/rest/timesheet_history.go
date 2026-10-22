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

	// ReplacedBy is the manager whose correction replaced this version, left out
	// where the owner made it; their name is empty once they are erased.
	ReplacedBy *ActedByResponse `json:"replacedBy,omitempty"`
}

// ActedByResponse is an account that did something in somebody else's name.
type ActedByResponse struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
}

// Revisions handles GET /api/v1/timesheets/{id}/revisions: what an entry said
// before each correction, the most recently replaced first.
//
// For its owner, and for a manager who may read the owner's time. Anybody else is
// told the entry is not there, which is what they are told about the entry itself.
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

	if err := h.authz.requireOwnEntry(c, principal, id, history.OwnerID, model.PermTimesheetReadTeam); err != nil {
		return nil, err
	}

	items := make([]TimesheetRevisionResponse, 0, len(history.Revisions))

	for _, revision := range history.Revisions {
		item := TimesheetRevisionResponse{
			Date: Date{Time: model.CalendarDay(revision.Date)}, DurationHours: revision.DurationHours,
			ProjectID: revision.ProjectID, Description: revision.Description,
			ReplacedAt: revision.ReplacedAt.UTC(),
		}

		if revision.ReplacedBy != nil {
			item.ReplacedBy = &ActedByResponse{ID: *revision.ReplacedBy, Name: history.Names[*revision.ReplacedBy]}
		}

		items = append(items, item)
	}

	return listResponse[TimesheetRevisionResponse]{Items: items, TotalCount: uint(len(items))}, nil
}
