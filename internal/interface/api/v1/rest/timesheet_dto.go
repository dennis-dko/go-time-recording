package rest

import (
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/common"
)

// TimesheetResponse is the wire representation of a time entry.
type TimesheetResponse struct {
	ID     uint `json:"id"`
	UserID uint `json:"userId"`

	// null when the entry has not been assigned to a project.
	ProjectID *uint `json:"projectId"`

	Date          Date    `json:"date"`
	DurationHours float64 `json:"durationHours"`
	Description   *string `json:"description"`

	// When the entry was recorded, and when it was last corrected. Null together
	// for one booked before those were kept.
	//
	// time.Time rather than this package's Date, which formats a day and throws the
	// rest away - right for the day the work was done and wrong for these, where a
	// correction made the same afternoon would otherwise be indistinguishable from
	// the booking it corrected. RFC 3339, in UTC, as stored.
	CreatedAt *time.Time `json:"createdAt"`
	UpdatedAt *time.Time `json:"updatedAt"`
}

// CreateTimesheetRequest is the payload for booking time.
type CreateTimesheetRequest struct {
	UserID        uint    `json:"userId"`
	ProjectID     uint    `json:"projectId"`
	Date          Date    `json:"date"`
	DurationHours float64 `json:"durationHours"`
	Description   *string `json:"description"`
}

// UpdateTimesheetRequest is the payload for a partial update.
type UpdateTimesheetRequest struct {
	UserID        *uint    `json:"userId"`
	ProjectID     *uint    `json:"projectId"`
	Date          *Date    `json:"date"`
	DurationHours *float64 `json:"durationHours"`
	Description   *string  `json:"description"`
}

// ReportEntry is one row of a per-project time report.
type ReportEntry struct {
	UserID uint    `json:"userId"`
	Hours  float64 `json:"hours"`
}

// ReportResponse totals booked hours per user for one project.
type ReportResponse struct {
	ProjectID  uint          `json:"projectId"`
	From       Date          `json:"from"`
	To         Date          `json:"to"`
	Entries    []ReportEntry `json:"entries"`
	TotalHours float64       `json:"totalHours"`
}

// TransferRequest moves a time entry to a different project.
type TransferRequest struct {
	ProjectID uint `json:"projectId"`
}

func newTimesheetResponse(r *common.TimesheetResult) TimesheetResponse {
	return TimesheetResponse{
		ID:            r.ID,
		UserID:        r.UserID,
		ProjectID:     r.ProjectID,
		Date:          Date{Time: r.Date},
		DurationHours: r.DurationHours,
		Description:   r.Description,
		CreatedAt:     momentOrNil(r.CreatedAt),
		UpdatedAt:     momentOrNil(r.UpdatedAt),
	}
}

func newTimesheetResponses(results []*common.TimesheetResult) []TimesheetResponse {
	out := make([]TimesheetResponse, 0, len(results))
	for _, r := range results {
		out = append(out, newTimesheetResponse(r))
	}

	return out
}
