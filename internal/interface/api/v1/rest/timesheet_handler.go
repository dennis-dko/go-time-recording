package rest

import (
	"cmp"
	"slices"
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/query"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	domainservice "github.com/dennis-dko/go-time-recording/internal/domain/service"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

// TimesheetHandler serves the time entry endpoints.
type TimesheetHandler struct {
	timesheets service.TimesheetService
	domain     *domainservice.TimesheetDomainService
	authz      *Authorizer
	timezone   InstanceTimezoneFunc
}

// NewTimesheetHandler creates a timesheet handler.
func NewTimesheetHandler(
	timesheets service.TimesheetService,
	domain *domainservice.TimesheetDomainService,
	authz *Authorizer,
	timezone InstanceTimezoneFunc,
) *TimesheetHandler {
	return &TimesheetHandler{
		timesheets: timesheets, domain: domain, authz: authz, timezone: timezone,
	}
}

// List handles GET /api/v1/timesheets, filtered by user, project, status and
// date range. A caller who may only see their own entries is pinned to their
// own id regardless of the filter they sent.
func (h *TimesheetHandler) List(c *gofr.Context) (any, error) {
	principal, err := h.authz.RequireAny(c, model.PermTimesheetReadOwn, model.PermTimesheetReadAll)
	if err != nil {
		return nil, err
	}

	requestedUserID, err := queryUint(c, "userId")
	if err != nil {
		return nil, toHTTPError(err)
	}

	userID, err := h.authz.scopeUserID(principal, requestedUserID)
	if err != nil {
		return nil, err
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
	principal, err := h.authz.RequireAny(c, model.PermTimesheetReadOwn, model.PermTimesheetReadAll)
	if err != nil {
		return nil, err
	}

	id, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	result, err := h.timesheets.GetTimesheet(c, query.GetTimesheetQuery{ID: id})
	if err != nil {
		return nil, toHTTPError(err)
	}

	if h.authz.Enabled() && !principal.Can(model.PermTimesheetReadAll) &&
		result.Result.UserID != principal.User.ID {
		return nil, forbiddenError{msg: "you may only read your own time entries"}
	}

	return newTimesheetResponse(result.Result), nil
}

// Create handles POST /api/v1/timesheets
func (h *TimesheetHandler) Create(c *gofr.Context) (any, error) {
	principal, err := h.authz.RequireAny(c, model.PermTimesheetWriteOwn, model.PermTimesheetWriteAll)
	if err != nil {
		return nil, err
	}

	var req CreateTimesheetRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	// Booking without naming a user means booking for yourself, which is what
	// the common case wants.
	if req.UserID == 0 && principal.User != nil {
		req.UserID = principal.User.ID
	}

	if err := h.authz.requireOwnerOrAll(principal, req.UserID); err != nil {
		return nil, err
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
	principal, err := h.authz.RequireAny(c, model.PermTimesheetWriteOwn, model.PermTimesheetWriteAll)
	if err != nil {
		return nil, err
	}

	id, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	existing, err := h.timesheets.GetTimesheet(c, query.GetTimesheetQuery{ID: id})
	if err != nil {
		return nil, toHTTPError(err)
	}

	if err := h.authz.requireOwnerOrAll(principal, existing.Result.UserID); err != nil {
		return nil, err
	}

	var req UpdateTimesheetRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	// Approving or rejecting is a separate right from editing your own hours.
	if req.Status != nil && isReviewDecision(*req.Status) {
		if _, err := h.authz.Require(c, model.PermTimesheetApprove); err != nil {
			return nil, err
		}
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

// isReviewDecision reports whether a status change is an approval decision
// rather than something the author may do themselves.
func isReviewDecision(status string) bool {
	return status == model.TimesheetStatusApproved || status == model.TimesheetStatusRejected
}

// Delete handles DELETE /api/v1/timesheets/{id}
func (h *TimesheetHandler) Delete(c *gofr.Context) (any, error) {
	principal, err := h.authz.RequireAny(c, model.PermTimesheetWriteOwn, model.PermTimesheetWriteAll)
	if err != nil {
		return nil, err
	}

	id, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	existing, err := h.timesheets.GetTimesheet(c, query.GetTimesheetQuery{ID: id})
	if err != nil {
		return nil, toHTTPError(err)
	}

	if err := h.authz.requireOwnerOrAll(principal, existing.Result.UserID); err != nil {
		return nil, err
	}

	if err := h.timesheets.DeleteTimesheet(c, command.DeleteTimesheetCommand{ID: id}); err != nil {
		return nil, toHTTPError(err)
	}

	return map[string]string{"status": "deleted"}, nil
}

// Transfer handles POST /api/v1/timesheets/{id}/transfer, moving an entry to
// another project via the domain service.
// viewerID is who is asking, or 0 when authentication is switched off - the
// same shape the project handler uses, and 0 means "sees everything", which is
// what a local trial with AUTH_ENABLED=false is.
func (h *TimesheetHandler) viewerID(principal *service.Principal) uint {
	if !h.authz.Enabled() || principal.User == nil {
		return 0
	}

	return principal.User.ID
}

func (h *TimesheetHandler) Transfer(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermTimesheetTransfer)
	if err != nil {
		return nil, err
	}

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

	// Who is asking, so a transfer onto somebody else's private category is
	// refused rather than performed.
	timesheet, err := h.domain.TransferTimesheetToProject(c, id, req.ProjectID, h.viewerID(principal))
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
	principal, err := h.authz.Require(c, model.PermReportRead)
	if err != nil {
		return nil, err
	}

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

	// In the reader's zone: "the last 30 days" ending on the server's idea of
	// today would be off by one for anyone far enough east or west.
	now := time.Now().In(principal.User.TimezoneOf(h.timezone.resolve(c)))
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

	perUser, err := h.domain.GenerateProjectTimeReport(c, projectID, *from, *to, h.viewerID(principal))
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
