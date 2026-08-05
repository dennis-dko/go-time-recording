package rest

import (
	"mime/multipart"

	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/http/response"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
	"github.com/dennis-dko/go-time-recording/internal/pkg/spreadsheet"
)

// WorkbookHandler serves the spreadsheet export and import.
type WorkbookHandler struct {
	workbook *service.WorkbookService
	authz    *Authorizer
	timezone InstanceTimezoneFunc
}

// NewWorkbookHandler creates the handler.
func NewWorkbookHandler(
	workbook *service.WorkbookService,
	authz *Authorizer,
	timezone InstanceTimezoneFunc,
) *WorkbookHandler {
	return &WorkbookHandler{workbook: workbook, authz: authz, timezone: timezone}
}

// xlsxContentType is what a browser needs to be told before it will hand the file
// to a spreadsheet application rather than trying to display it.
const xlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// Export handles GET /api/v1/timesheets/export.
//
// The same filters as the list, and the same scoping: whoever may only read their
// own entries exports their own, whatever they ask for. Reusing the list's own
// scoping rather than repeating it is the point - an export that saw more than the
// screen did would be a way around the screen.
//
// No filename is set here. GoFr's responder owns the headers and keeps its writer
// private, so Content-Disposition is not reachable from a handler; the interface
// names the file when it saves it, which it can do better anyway because it knows
// which period was asked for.
func (h *WorkbookHandler) Export(c *gofr.Context) (any, error) {
	principal, err := h.authz.RequireAny(c,
		model.PermTimesheetReadOwn, model.PermTimesheetReadAll)
	if err != nil {
		return nil, err
	}

	filter, err := h.timesheetFilter(c, principal)
	if err != nil {
		return nil, err
	}

	book, err := h.workbook.Export(c, filter)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return response.File{Content: book, ContentType: xlsxContentType}, nil
}

// timesheetFilter builds the filter from the same query parameters the list reads,
// with the same scoping applied to the user.
//
// scopeUserID is the security-relevant line: a caller who may only read their own
// entries is pinned to their own id whatever they asked for. The list endpoint calls
// the same function, which is what keeps the export from ever seeing more than the
// screen.
func (h *WorkbookHandler) timesheetFilter(
	c *gofr.Context,
	principal *service.Principal,
) (repository.TimesheetFilter, error) {
	var empty repository.TimesheetFilter

	requested, err := queryUint(c, "userId")
	if err != nil {
		return empty, toHTTPError(err)
	}

	userID, err := h.authz.scopeUserID(principal, requested)
	if err != nil {
		return empty, err
	}

	projectID, err := queryUint(c, "projectId")
	if err != nil {
		return empty, toHTTPError(err)
	}

	from, err := queryDate(c, "from")
	if err != nil {
		return empty, toHTTPError(err)
	}

	to, err := queryDate(c, "to")
	if err != nil {
		return empty, toHTTPError(err)
	}

	return repository.TimesheetFilter{
		UserID:    userID,
		ProjectID: projectID,
		Status:    c.Param("status"),
		StartDate: from,
		EndDate:   to,
	}, nil
}

// ImportRequest is the upload.
//
// dryRun is what makes the preview possible: the same request, parsed and planned
// the same way, and told not to write. The file is sent twice - once to look, once
// to do - rather than the server keeping a half-finished import between requests.
// A plan held server-side would need an owner, a lifetime and a way to expire, and
// re-reading a spreadsheet costs almost nothing.
type ImportRequest struct {
	File   multipart.FileHeader `file:"file"`
	DryRun bool                 `form:"dryRun"`
}

// ImportRowResponse is one row of the file, as it would be written or the reason it
// cannot be.
type ImportRowResponse struct {
	// Row is the line in the spreadsheet, so somebody can go and look at it. The
	// heading is row 1.
	Row int `json:"row"`

	Date        *Date   `json:"date"`
	User        string  `json:"user"`
	Project     string  `json:"project"`
	Hours       float64 `json:"hours"`
	Description string  `json:"description"`

	// Problem is empty for a row that would be written.
	Problem string `json:"problem"`
}

// ImportResponse is the whole file, understood.
type ImportResponse struct {
	// DryRun says whether anything was written, so the interface cannot report an
	// import that did not happen.
	DryRun bool `json:"dryRun"`

	Rows []ImportRowResponse `json:"rows"`

	Writable int `json:"writable"`
	Rejected int `json:"rejected"`

	// Imported is how many entries were actually created, which is zero for a
	// preview and for a file that was refused.
	Imported int `json:"imported"`
}

// Import handles POST /api/v1/timesheets/import.
//
// Writing somebody else's hours needs the wider permission, which is checked per
// row rather than here: a file of one's own time is the ordinary case and must not
// need the right to book for others.
func (h *WorkbookHandler) Import(c *gofr.Context) (any, error) {
	principal, err := h.authz.RequireAny(c,
		model.PermTimesheetWriteOwn, model.PermTimesheetWriteAll)
	if err != nil {
		return nil, err
	}

	var req ImportRequest
	if err := c.Bind(&req); err != nil {
		return nil, toHTTPError(apperror.Invalidf("the upload could not be read: %v", err).
			WithCode("uploadUnreadable"))
	}

	if req.File.Size == 0 {
		return nil, toHTTPError(apperror.Invalidf("no file was uploaded").
			WithCode("noFileUploaded"))
	}

	file, err := req.File.Open()
	if err != nil {
		return nil, toHTTPError(apperror.Invalidf("the upload could not be opened: %v", err).
			WithCode("uploadUnreadable"))
	}

	defer func() { _ = file.Close() }()

	rows, problems, err := spreadsheet.Read(file)
	if err != nil {
		// A file that is not a workbook at all, as opposed to one with bad rows in
		// it: there is nothing to preview and nothing to fix row by row.
		return nil, toHTTPError(apperror.Invalidf(
			"this is not a readable .xlsx workbook: %v", err).WithCode("notAWorkbook"))
	}

	// With enforcement off there is no caller to restrict, which is the same
	// reading every other handler takes.
	mayWriteAll := !h.authz.Enabled() || principal.Can(model.PermTimesheetWriteAll)

	plan, err := h.workbook.Plan(c, rows, problems, principal.User, mayWriteAll)
	if err != nil {
		return nil, toHTTPError(err)
	}

	out := ImportResponse{
		DryRun:   req.DryRun,
		Rows:     make([]ImportRowResponse, 0, len(plan.Rows)),
		Writable: plan.Writable,
		Rejected: plan.Rejected,
	}

	for _, row := range plan.Rows {
		item := ImportRowResponse{
			Row: row.Number, User: row.UserName, Project: row.ProjectName,
			Hours: row.Hours, Description: row.Description, Problem: row.Problem,
		}

		// Null rather than the zero date for a row whose date could not be read,
		// so the interface shows a gap instead of the first of January in year one.
		if !row.Date.IsZero() {
			item.Date = &Date{Time: row.Date}
		}

		out.Rows = append(out.Rows, item)
	}

	if req.DryRun {
		return out, nil
	}

	imported, err := h.workbook.Apply(c, plan)
	if err != nil {
		return nil, toHTTPError(err)
	}

	out.Imported = imported

	return out, nil
}
