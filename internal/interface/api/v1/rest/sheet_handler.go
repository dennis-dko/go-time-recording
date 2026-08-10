package rest

import (
	"errors"
	"mime/multipart"
	"strings"

	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/http/response"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
	"github.com/dennis-dko/go-time-recording/internal/pkg/spreadsheet"
)

// SheetHandler serves the export and import of projects and people.
//
// Separate from WorkbookHandler, which serves time entries and predates the idea
// that every table has its own. Both answers carry their own column headings, and
// projects and people answer in exactly the same shape, so one preview in the
// interface renders either of them.
type SheetHandler struct {
	projects *service.ProjectWorkbookService
	users    *service.UserWorkbookService
	authz    *Authorizer
}

// NewSheetHandler creates the handler.
func NewSheetHandler(
	projects *service.ProjectWorkbookService,
	users *service.UserWorkbookService,
	authz *Authorizer,
) *SheetHandler {
	return &SheetHandler{projects: projects, users: users, authz: authz}
}

// SheetImportRow is one row of a file, as it would be written or the reason it
// cannot be.
//
// Cells as text in the sheet's own column order, rather than named fields. It is
// what lets one preview in the interface show any of these imports: the columns
// come with the answer, already translated, so the preview needs to know nothing
// about what kind of file it is looking at.
type SheetImportRow struct {
	// Row is the line in the spreadsheet, so somebody can go and look. The heading
	// is row 1.
	Row int `json:"row"`

	Cells []string `json:"cells"`

	// Problem is empty for a row that would be written.
	Problem string `json:"problem"`
}

// SheetImportResponse is a whole file, understood.
type SheetImportResponse struct {
	// DryRun says whether anything was written, so the interface cannot report an
	// import that did not happen.
	DryRun bool `json:"dryRun"`

	// Columns are the headings, in the language that was asked for.
	Columns []string `json:"columns"`

	Rows []SheetImportRow `json:"rows"`

	Writable int `json:"writable"`
	Rejected int `json:"rejected"`

	// Imported is how many rows were actually written, which is zero for a preview
	// and for a file that was refused.
	Imported int `json:"imported"`
}

// language reads the language the file should be written in.
//
// From the request rather than from the account: the export is a file somebody is
// about to open, and the language they want it in is the language they are reading
// the screen in - which is what the interface sends. An unknown value falls back to
// English inside the spreadsheet package, so nothing has to be validated here.
func language(c *gofr.Context) string { return strings.TrimSpace(c.Param("lang")) }

// ExportProjects handles GET /api/v1/projects/export.
//
// Scoped to what the caller may see, by going through the same list the screen
// uses: an export that showed more than the screen would be a way around the
// screen.
//
// No filename is set here. GoFr's responder owns the headers and keeps its writer
// private, so Content-Disposition is not reachable from a handler; the interface
// names the file when it saves it.
func (h *SheetHandler) ExportProjects(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermProjectRead)
	if err != nil {
		return nil, err
	}

	book, err := h.projects.Export(c, language(c), h.viewerID(principal))
	if err != nil {
		return nil, toHTTPError(err)
	}

	return response.File{Content: book, ContentType: xlsxContentType}, nil
}

// viewerID is whose eyes the list is read through.
//
// Zero with enforcement switched off, which the list reads as "no scoping" - the
// same reading every other handler takes.
func (h *SheetHandler) viewerID(principal *service.Principal) uint {
	if !h.authz.Enabled() || principal == nil || principal.User == nil {
		return 0
	}

	return principal.User.ID
}

// ImportProjects handles POST /api/v1/projects/import.
//
// Every row becomes a project of the importer's own, because that is the only kind
// there is. There used to be two, and a row asking for the shared kind was refused by
// name in the preview when the caller could only keep private ones - a distinction
// with nothing left on either side of it.
func (h *SheetHandler) ImportProjects(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermProjectWrite)
	if err != nil {
		return nil, err
	}

	sent, err := uploadedFile(c)
	if err != nil {
		return nil, err
	}

	defer sent.close()

	rows, problems, err := spreadsheet.ReadProjects(sent.file)
	if err != nil {
		return nil, unreadableWorkbook(err)
	}

	plan, err := h.projects.PlanProjects(c, language(c), rows, problems, principal.User)
	if err != nil {
		return nil, toHTTPError(err)
	}

	out := sheetResponse(sent.dryRun, &plan.SheetPlan)
	if sent.dryRun {
		return out, nil
	}

	imported, err := h.projects.ApplyProjects(c, plan, principal.User)
	if err != nil {
		return nil, toHTTPError(err)
	}

	out.Imported = imported

	return out, nil
}

// ExportUsers handles GET /api/v1/users/export.
func (h *SheetHandler) ExportUsers(c *gofr.Context) (any, error) {
	if _, err := h.authz.Require(c, model.PermUserRead); err != nil {
		return nil, err
	}

	book, err := h.users.Export(c, language(c))
	if err != nil {
		return nil, toHTTPError(err)
	}

	return response.File{Content: book, ContentType: xlsxContentType}, nil
}

// ImportUsers handles POST /api/v1/users/import.
//
// Changes accounts and does not create them: see UserWorkbookService for why a
// spreadsheet is the wrong place for a password.
func (h *SheetHandler) ImportUsers(c *gofr.Context) (any, error) {
	if _, err := h.authz.Require(c, model.PermUserWrite); err != nil {
		return nil, err
	}

	sent, err := uploadedFile(c)
	if err != nil {
		return nil, err
	}

	defer sent.close()

	rows, problems, err := spreadsheet.ReadUsers(sent.file)
	if err != nil {
		return nil, unreadableWorkbook(err)
	}

	plan, err := h.users.PlanUsers(c, language(c), rows, problems)
	if err != nil {
		return nil, toHTTPError(err)
	}

	out := sheetResponse(sent.dryRun, &plan.SheetPlan)
	if sent.dryRun {
		return out, nil
	}

	imported, err := h.users.ApplyUsers(c, plan)
	if err != nil {
		return nil, toHTTPError(err)
	}

	out.Imported = imported

	return out, nil
}

// sheetResponse turns a plan into the answer.
func sheetResponse(dryRun bool, plan *service.SheetPlan) SheetImportResponse {
	out := SheetImportResponse{
		DryRun:   dryRun,
		Columns:  plan.Columns,
		Rows:     make([]SheetImportRow, 0, len(plan.Rows)),
		Writable: plan.Writable,
		Rejected: plan.Rejected,
	}

	for _, row := range plan.Rows {
		cells := row.Cells
		if cells == nil {
			// A row the file could not be read for at all has no cells to show.
			// An empty list rather than null, so the interface can loop over it
			// without asking.
			cells = []string{}
		}

		out.Rows = append(out.Rows, SheetImportRow{
			Row: row.Number, Cells: cells, Problem: row.Problem,
		})
	}

	return out
}

// upload is a file somebody sent, opened.
type upload struct {
	file   multipart.File
	dryRun bool
	close  func()
}

// uploadedFile reads the upload out of the request.
//
// Two handlers doing the same four checks in the same order is two chances to leave
// one out, and the one that gets left out is the size check on an empty upload.
//
// dryRun comes back with it rather than being read separately from the query, so
// there is one answer to "is this a preview" per request. Two sources for that would
// eventually disagree, and the disagreement writes a file somebody only meant to
// look at.
func uploadedFile(c *gofr.Context) (*upload, error) {
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
		return nil, toHTTPError(apperror.Invalidf(
			"the upload could not be opened: %v", err).WithCode("uploadUnreadable"))
	}

	return &upload{file: file, dryRun: req.DryRun, close: func() { _ = file.Close() }}, nil
}

// unreadableWorkbook is the answer to a file that is not a workbook of this kind
// at all, as opposed to one with bad rows in it: there is nothing to preview and
// nothing to fix row by row.
func unreadableWorkbook(err error) error {
	if errors.Is(err, spreadsheet.ErrWrongSheet) {
		return toHTTPError(apperror.Invalidf(
			"this workbook holds something else: %v", err).WithCode("wrongWorkbook"))
	}

	return toHTTPError(apperror.Invalidf(
		"this is not a readable .xlsx workbook: %v", err).WithCode("notAWorkbook"))
}
