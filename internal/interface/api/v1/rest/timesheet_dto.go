package rest

import "github.com/dennis-dko/go-time-recording/internal/application/v1/common"

// TimesheetResponse is the wire representation of a time entry.
type TimesheetResponse struct {
	ID            uint    `json:"id"`
	UserID        uint    `json:"userId"`
	ProjectID     uint    `json:"projectId"`
	Date          Date    `json:"date"`
	DurationHours float64 `json:"durationHours"`
	Description   *string `json:"description"`
	Status        string  `json:"status"`
}

// CreateTimesheetRequest is the payload for booking time.
type CreateTimesheetRequest struct {
	UserID        uint    `json:"userId"`
	ProjectID     uint    `json:"projectId"`
	Date          Date    `json:"date"`
	DurationHours float64 `json:"durationHours"`
	Description   *string `json:"description"`
	Status        string  `json:"status"`
}

// UpdateTimesheetRequest is the payload for a partial update.
type UpdateTimesheetRequest struct {
	UserID        *uint    `json:"userId"`
	ProjectID     *uint    `json:"projectId"`
	Date          *Date    `json:"date"`
	DurationHours *float64 `json:"durationHours"`
	Description   *string  `json:"description"`
	Status        *string  `json:"status"`
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
		Status:        r.Status,
	}
}

func newTimesheetResponses(results []*common.TimesheetResult) []TimesheetResponse {
	out := make([]TimesheetResponse, 0, len(results))
	for _, r := range results {
		out = append(out, newTimesheetResponse(r))
	}

	return out
}
