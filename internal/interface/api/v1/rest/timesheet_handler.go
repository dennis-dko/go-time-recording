package rest

import (
	"cmp"
	"slices"
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/query"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	domainservice "github.com/dennis-dko/go-time-recording/internal/domain/service"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

// TimesheetHandler serves the time entry endpoints.
type TimesheetHandler struct {
	timesheets service.TimesheetService
	domain     *domainservice.TimesheetDomainService
}

// NewTimesheetHandler creates a timesheet handler.
func NewTimesheetHandler(
	timesheets service.TimesheetService,
	domain *domainservice.TimesheetDomainService,
) *TimesheetHandler {
	return &TimesheetHandler{timesheets: timesheets, domain: domain}
}

// List handles GET /api/v1/timesheets, filtered by user, project, status and
// date range.
func (h *TimesheetHandler) List(c *gofr.Context) (any, error) {
	userID, err := queryUint(c, "userId")
	if err != nil {
		return nil, toHTTPError(err)
	}

	projectID, err := queryUint(c, "projectId")
	if err != nil {
		return nil, toHTTPError(err)
	}

	from, err := queryDate(c, "from")
	if err != nil {
		return nil, toHTTPError(err)
	}

	to, err := queryDate(c, "to")
	if err != nil {
		return nil, toHTTPError(err)
	}

	result, err := h.timesheets.ListTimesheets(c, query.ListTimesheetsQuery{
		UserID:    userID,
		ProjectID: projectID,
		Status:    c.Param("status"),
		StartDate: from,
		EndDate:   to,
	})
	if err != nil {
		return nil, toHTTPError(err)
	}

	return listResponse[TimesheetResponse]{
		Items:      newTimesheetResponses(result.Result),
		TotalCount: result.TotalCount,
	}, nil
}

// Get handles GET /api/v1/timesheets/{id}
func (h *TimesheetHandler) Get(c *gofr.Context) (any, error) {
	id, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	result, err := h.timesheets.GetTimesheet(c, query.GetTimesheetQuery{ID: id})
	if err != nil {
		return nil, toHTTPError(err)
	}

	return newTimesheetResponse(result.Result), nil
}

// Create handles POST /api/v1/timesheets
func (h *TimesheetHandler) Create(c *gofr.Context) (any, error) {
	var req CreateTimesheetRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	result, err := h.timesheets.CreateTimesheet(c, command.CreateTimesheetCommand{
		UserID:        req.UserID,
		ProjectID:     req.ProjectID,
		Date:          req.Date.Time,
		DurationHours: req.DurationHours,
		Description:   req.Description,
		Status:        req.Status,
	})
	if err != nil {
		return nil, toHTTPError(err)
	}

	return newTimesheetResponse(result.Result), nil
}

// Update handles PUT /api/v1/timesheets/{id}
func (h *TimesheetHandler) Update(c *gofr.Context) (any, error) {
	id, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	var req UpdateTimesheetRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	cmd := command.UpdateTimesheetCommand{
		ID:            id,
		UserID:        req.UserID,
		ProjectID:     req.ProjectID,
		DurationHours: req.DurationHours,
		Description:   req.Description,
		Status:        req.Status,
	}

	if req.Date != nil {
		date := req.Date.Time
		cmd.Date = &date
	}

	result, err := h.timesheets.UpdateTimesheet(c, cmd)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return newTimesheetResponse(result.Result), nil
}

// Delete handles DELETE /api/v1/timesheets/{id}
func (h *TimesheetHandler) Delete(c *gofr.Context) (any, error) {
	id, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	if err := h.timesheets.DeleteTimesheet(c, command.DeleteTimesheetCommand{ID: id}); err != nil {
		return nil, toHTTPError(err)
	}

	return map[string]string{"status": "deleted"}, nil
}

// Transfer handles POST /api/v1/timesheets/{id}/transfer, moving an entry to
// another project via the domain service.
func (h *TimesheetHandler) Transfer(c *gofr.Context) (any, error) {
	id, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	var req TransferRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	if req.ProjectID == 0 {
		return nil, toHTTPError(apperror.InvalidFields("projectId"))
	}

	timesheet, err := h.domain.TransferTimesheetToProject(c, id, req.ProjectID)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return TimesheetResponse{
		ID:            timesheet.ID,
		UserID:        timesheet.UserID,
		ProjectID:     timesheet.ProjectID,
		Date:          Date{Time: timesheet.Date},
		DurationHours: timesheet.DurationHours,
		Description:   timesheet.Description,
		Status:        timesheet.Status,
	}, nil
}

// Report handles GET /api/v1/projects/{id}/report, totalling booked hours per
// user over a date range. The range defaults to the last 30 days.
func (h *TimesheetHandler) Report(c *gofr.Context) (any, error) {
	projectID, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	from, err := queryDate(c, "from")
	if err != nil {
		return nil, toHTTPError(err)
	}

	to, err := queryDate(c, "to")
	if err != nil {
		return nil, toHTTPError(err)
	}

	now := time.Now()
	if to == nil {
		to = &now
	}

	if from == nil {
		start := to.AddDate(0, 0, -30)
		from = &start
	}

	if to.Before(*from) {
		return nil, toHTTPError(apperror.Invalidf("'to' must not be before 'from'"))
	}

	perUser, err := h.domain.GenerateProjectTimeReport(c, projectID, *from, *to)
	if err != nil {
		return nil, toHTTPError(err)
	}

	resp := ReportResponse{
		ProjectID: projectID,
		From:      Date{Time: *from},
		To:        Date{Time: *to},
		Entries:   make([]ReportEntry, 0, len(perUser)),
	}

	for userID, hours := range perUser {
		resp.Entries = append(resp.Entries, ReportEntry{UserID: userID, Hours: hours})
		resp.TotalHours += hours
	}

	// Map iteration order is random; sort so the response is deterministic.
	slices.SortFunc(resp.Entries, func(a, b ReportEntry) int {
		return cmp.Compare(a.UserID, b.UserID)
	})

	return resp, nil
}
