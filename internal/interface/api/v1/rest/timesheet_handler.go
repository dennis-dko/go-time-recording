package rest

import (
	"cmp"
	"slices"
	"strings"
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
	principal, err := h.authz.Require(c, model.PermTimesheetReadOwn)
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

	// "none" rather than a number, matching what the evaluation already takes: a
	// zero id means "any project", so the entries that belong to none cannot be
	// asked for with a number at all.
	projectID, withoutProject, err := projectFilter(c)
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
		UserID:         userID,
		ProjectID:      projectID,
		WithoutProject: withoutProject,
		StartDate:      from,
		EndDate:        to,
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
	principal, err := h.authz.Require(c, model.PermTimesheetReadOwn)
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

	if h.authz.Enabled() && result.Result.UserID != principal.User.ID {
		return nil, forbiddenError{msg: "you may only read your own time entries"}.
			WithCode("onlyOwnEntriesRead")
	}

	return newTimesheetResponse(result.Result), nil
}

// Create handles POST /api/v1/timesheets
func (h *TimesheetHandler) Create(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermTimesheetWriteOwn)
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

	if err := h.authz.requireOwner(principal, req.UserID); err != nil {
		return nil, err
	}

	result, err := h.timesheets.CreateTimesheet(c, command.CreateTimesheetCommand{
		UserID:        req.UserID,
		ProjectID:     req.ProjectID,
		Date:          req.Date.Time,
		DurationHours: req.DurationHours,
		Description:   req.Description,
	})
	if err != nil {
		return nil, toHTTPError(err)
	}

	return newTimesheetResponse(result.Result), nil
}

// Update handles PUT /api/v1/timesheets/{id}
func (h *TimesheetHandler) Update(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermTimesheetWriteOwn)
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

	if err := h.authz.requireOwner(principal, existing.Result.UserID); err != nil {
		return nil, err
	}

	var req UpdateTimesheetRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	// And whose it would become. The check above is about the entry as it stands;
	// without this one, somebody who may only write their own could hand an entry
	// to a colleague by naming them here, pushing hours onto an account that is not
	// theirs to book for. Create checks the target the same way.
	if req.UserID != nil {
		if err := h.authz.requireOwner(principal, *req.UserID); err != nil {
			return nil, err
		}
	}

	cmd := command.UpdateTimesheetCommand{
		ID:            id,
		UserID:        req.UserID,
		ProjectID:     req.ProjectID,
		DurationHours: req.DurationHours,
		Description:   req.Description,
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
	principal, err := h.authz.Require(c, model.PermTimesheetWriteOwn)
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

	if err := h.authz.requireOwner(principal, existing.Result.UserID); err != nil {
		return nil, err
	}

	if err := h.timesheets.DeleteTimesheet(c, command.DeleteTimesheetCommand{ID: id}); err != nil {
		return nil, toHTTPError(err)
	}

	return map[string]string{"status": "deleted"}, nil
}

// Transfer handles POST /api/v1/timesheets/{id}/transfer, moving an entry to
// another project via the domain service.
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

	// Whose entry it is, which this did not ask.
	//
	// It checked the target project and nothing about the entry, so the permission
	// alone let anybody holding it move a colleague's hours onto another project -
	// changing that colleague's totals - and read the entry's date, hours and
	// description back out of the response. Ids are small and sequential, so
	// walking somebody else's week cost nothing. Create, Update and Delete have all
	// checked this from the start; only this path did not.
	existing, err := h.timesheets.GetTimesheet(c, query.GetTimesheetQuery{ID: id})
	if err != nil {
		return nil, toHTTPError(err)
	}

	if err := h.authz.requireOwnEntry(principal, existing.Result.UserID); err != nil {
		return nil, err
	}

	// Who is asking, so a transfer onto somebody else's private category is
	// refused rather than performed.
	timesheet, err := h.domain.TransferTimesheetToProject(c, id, req.ProjectID, h.authz.viewerID(principal))
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
	}, nil
}

// OwnReport handles GET /api/v1/reports, totalling what the caller booked over a
// date range. The range defaults to the last 30 days.
//
// projectId chooses what it covers: absent or empty is every project, the literal
// "none" is the hours that were never given one, and a number is that project.
//
// It exists beside Report rather than replacing it because the two are different
// questions, and because an evaluation used to demand exactly one project - the
// screen's picker was required and the endpoint parsed an id out of the path - so
// "everything I did" and "everything I did that is on nothing" were both
// unaskable, which is precisely what people wanted to ask.
func (h *TimesheetHandler) OwnReport(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermReportReadOwn)
	if err != nil {
		return nil, err
	}

	scope, err := reportScope(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	from, to, err := h.reportRange(c, principal)
	if err != nil {
		return nil, err
	}

	total, err := h.domain.GenerateOwnTimeReport(c, scope, *from, *to, principal.User.ID)
	if err != nil {
		return nil, toHTTPError(err)
	}

	resp := ReportResponse{
		ProjectID:  scope.ProjectID,
		From:       Date{Time: *from},
		To:         Date{Time: *to},
		Entries:    make([]ReportEntry, 0, 1),
		TotalHours: total,
	}

	// The same shape a project report answers with, so one screen renders both.
	// Nothing booked stays an empty list rather than a zero row: "no bookings in
	// this period" and "0.00 h" say the same thing, and only one of them reads
	// like an answer.
	if total > 0 {
		resp.Entries = append(resp.Entries, ReportEntry{UserID: principal.User.ID, Hours: total})
	}

	return resp, nil
}

// reportScope reads which projects an evaluation covers.
//
// "none" rather than an empty value for the unassigned hours, because empty is
// already taken by "all of them" - a select that submits nothing and a select
// nobody touched are the same string.
func reportScope(c *gofr.Context) (domainservice.ProjectScope, error) {
	raw := strings.TrimSpace(c.Param("projectId"))

	switch raw {
	case "":
		return domainservice.ProjectScope{}, nil
	case unassignedProjects:
		return domainservice.ProjectScope{Unassigned: true}, nil
	}

	id, err := parseUint(raw, "projectId")
	if err != nil {
		return domainservice.ProjectScope{}, err
	}

	return domainservice.ProjectScope{ProjectID: id}, nil
}

// unassignedProjects is what the interface sends for "no project assigned". It is
// a word rather than a number because there is no id that means "not one of them".
const unassignedProjects = "none"

// reportRange reads the from/to pair an evaluation covers, defaulting to the last
// 30 days in the reader's own zone.
func (h *TimesheetHandler) reportRange(
	c *gofr.Context,
	principal *service.Principal,
) (*time.Time, *time.Time, error) {
	from, err := queryDate(c, "from")
	if err != nil {
		return nil, nil, toHTTPError(err)
	}

	to, err := queryDate(c, "to")
	if err != nil {
		return nil, nil, toHTTPError(err)
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
		return nil, nil, toHTTPError(
			apperror.Invalidf("'to' must not be before 'from'").WithCode("rangeInverted"))
	}

	return from, to, nil
}

// Report handles GET /api/v1/projects/{id}/report, totalling booked hours over a
// date range. The range defaults to the last 30 days.
//
// The caller's own hours. It used to be everybody's for anyone who could open it at
// all, gated on a right that no role held - so the screen was unreachable and, had
// anyone granted it, would have shown what each colleague had booked on the project.
// Nobody sees what anybody else has.
func (h *TimesheetHandler) Report(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermReportReadOwn)
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
		return nil, toHTTPError(apperror.Invalidf("'to' must not be before 'from'").WithCode("rangeInverted"))
	}

	perUser, err := h.domain.GenerateProjectTimeReport(c, projectID, *from, *to,
		h.authz.viewerID(principal), h.authz.reportScope(principal))
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

// projectFilter reads the projectId query parameter, which asks one of three
// questions.
//
// Absent or empty is every project. The literal "none" is the entries that were
// never given one - a question a number cannot express, because zero already
// means "any". Anything else is that project.
//
// The same three the evaluation takes, so one screen does not learn a different
// vocabulary from the other.
func projectFilter(c *gofr.Context) (projectID uint, withoutProject bool, err error) {
	raw := strings.TrimSpace(c.Param("projectId"))

	switch raw {
	case "":
		return 0, false, nil
	case "none":
		return 0, true, nil
	}

	id, err := parseUint(raw, "projectId")
	if err != nil {
		return 0, false, err
	}

	return id, false, nil
}
