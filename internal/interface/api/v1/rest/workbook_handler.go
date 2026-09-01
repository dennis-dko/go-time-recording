package rest

import (
	"mime/multipart"

	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/http/response"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
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
	principal, err := h.authz.Require(c, model.PermTimesheetReadOwn)
	if err != nil {
		return nil, err
	}

	filter, err := h.timesheetFilter(c, principal)
	if err != nil {
		return nil, err
	}

	book, err := h.workbook.Export(c, filter, language(c))
	if err != nil {
		return nil, toHTTPError(err)
	}

	return response.File{Content: book, ContentType: xlsxContentType}, nil
}

// timesheetFilter builds the repository filter from the scope the list reads.
//
// Through timesheetScopeOf rather than by parsing the query again, which is what
// keeps the export from ever seeing more than the screen it was started from -
// see the comment there for why that is a shared function and not two agreeing
// ones.
func (h *WorkbookHandler) timesheetFilter(
	c *gofr.Context,
	principal *service.Principal,
) (repository.TimesheetFilter, error) {
	scope, err := timesheetScopeOf(c, h.authz, principal)
	if err != nil {
		return repository.TimesheetFilter{}, err
	}

	return repository.TimesheetFilter{
		UserID:         scope.UserID,
		ProjectID:      scope.ProjectID,
		WithoutProject: scope.WithoutProject,
		StartDate:      scope.From,
		EndDate:        scope.To,
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

	// ProblemCode names which refusal it is and ProblemValues are what the English
	// sentence interpolated, so a client can say the same thing in its reader's
	// language. Omitted for a row with nothing wrong with it.
	ProblemCode   string `json:"problemCode,omitempty"`
	ProblemValues []any  `json:"problemValues,omitempty"`
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
// A row naming somebody else is rejected rather than refused wholesale, which is
// what the per-row plan is for: a file that is mostly yours still imports, and the
// rows that are not yours come back named, so whoever exported the file can see why.
func (h *WorkbookHandler) Import(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermTimesheetWriteOwn)
	if err != nil {
		return nil, err
	}

	sent, err := uploadedFile(c)
	if err != nil {
		return nil, err
	}

	defer sent.close()

	rows, problems, err := spreadsheet.Read(sent.file)
	if err != nil {
		return nil, unreadableWorkbook(err)
	}

	// With enforcement off there is no caller to restrict, which is the same
	// reading every other handler takes. With it on, the answer is now always no:
	// booking time in another person's name was the last thing the manager could do.
	mayWriteAll := !h.authz.Enabled()

	plan, err := h.workbook.Plan(c, rows, problems, principal.User, mayWriteAll)
	if err != nil {
		return nil, toHTTPError(err)
	}

	out := ImportResponse{
		DryRun:   sent.dryRun,
		Rows:     make([]ImportRowResponse, 0, len(plan.Rows)),
		Writable: plan.Writable,
		Rejected: plan.Rejected,
	}

	for _, row := range plan.Rows {
		item := ImportRowResponse{
			Row: row.Number, User: row.UserName, Project: row.ProjectName,
			Hours: row.Hours, Description: row.Description, Problem: row.Problem,
			ProblemCode: row.Code, ProblemValues: row.Values,
		}

		// Null rather than the zero date for a row whose date could not be read,
		// so the interface shows a gap instead of the first of January in year one.
		if !row.Date.IsZero() {
			item.Date = &Date{Time: row.Date}
		}

		out.Rows = append(out.Rows, item)
	}

	if sent.dryRun {
		return out, nil
	}

	imported, err := h.workbook.Apply(c, plan)
	if err != nil {
		return nil, toHTTPError(err)
	}

	out.Imported = imported

	return out, nil
}
