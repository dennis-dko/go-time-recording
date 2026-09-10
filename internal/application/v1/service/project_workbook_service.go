package service

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/common"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/query"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
	"github.com/dennis-dko/go-time-recording/internal/pkg/spreadsheet"
)

// ProjectWorkbookService moves projects in and out as a spreadsheet.
//
// It writes through the project service rather than the repository, so an imported
// project passes the same validation, the same status rules and the same ownership
// check as one created through the form. An importer with its own idea of what is
// valid would be a second, quieter API - and the quiet one is the one nobody
// checks.
type ProjectWorkbookService struct {
	projects *ProjectApplicationService
}

// NewProjectWorkbookService is given the project service rather than a
// repository, which is what holds an import to the rules the form is held to.
func NewProjectWorkbookService(projects *ProjectApplicationService) *ProjectWorkbookService {
	return &ProjectWorkbookService{projects: projects}
}

// Export writes the projects the viewer may see as a workbook.
//
// Scoped through the same list the screen uses, so an export can never show more
// than the screen did - which is the whole reason to reuse it rather than read the
// repository here.
func (s *ProjectWorkbookService) Export(
	ctx context.Context,
	language string,
	viewerID uint,
) ([]byte, error) {
	listed, err := s.projects.ListProjects(ctx, query.ListProjectsQuery{ViewerID: viewerID})
	if err != nil {
		return nil, err
	}

	rows := make([]spreadsheet.ProjectRow, 0, len(listed.Result))

	for _, project := range listed.Result {
		row := spreadsheet.ProjectRow{
			Name:      project.Name,
			StartDate: project.StartDate,
			Status:    project.Status,
		}

		if project.Description != nil {
			row.Description = *project.Description
		}

		if project.EndDate != nil {
			row.EndDate = *project.EndDate
		}

		rows = append(rows, row)
	}

	// By name, which is the order the sheet is read in and the only one that is
	// stable across exports.
	sort.SliceStable(rows, func(i, j int) bool {
		return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name)
	})

	return spreadsheet.WriteProjects(language, rows)
}

// plannedProject is one row, resolved: the project it refers to, if it exists.
type plannedProject struct {
	row spreadsheet.ProjectRow

	// existingID is zero for a row that would create a project.
	existingID uint

	// existingStatus is what that project's status is now, so a row that does not
	// change it can say nothing about it.
	existingStatus string
}

// ProjectPlan is a file of projects, understood.
//
// The resolved rows are kept unexported beside the preview: the handler hands this
// straight back to Apply, and nothing outside this package has any business
// rewriting what was planned between the preview and the write.
type ProjectPlan struct {
	SheetPlan

	writable []plannedProject
}

// PlanProjects works out what a file of projects would do, without writing.
//
// Matched by name, because a name is what somebody editing a spreadsheet can see
// and type. A name that is already there is an edit, one that is not is a new
// project - so importing the same file twice changes nothing the second time, which
// is what makes a half-finished import recoverable by simply importing again.
//
// Every row becomes a project of the actor's own, because that is the only kind there
// is. There was a mayShare beside this, deciding whether they were allowed to touch
// the shared sort at all - a question with nothing left on either side of it.
func (s *ProjectWorkbookService) PlanProjects(
	ctx context.Context,
	language string,
	rows []spreadsheet.ProjectRow,
	problems []spreadsheet.RowError,
	actor *model.User,
) (*ProjectPlan, error) {
	if actor == nil {
		return nil, apperror.InvalidFields("actor")
	}

	existing, err := s.projects.ListProjects(ctx, query.ListProjectsQuery{ViewerID: actor.ID})
	if err != nil {
		return nil, err
	}

	// One index: the list is already only this person's projects, so a name in it can
	// mean one thing. There were two, because a private category and a shared project
	// could share a name without being the same project.
	byName := map[string]*common.ProjectResult{}

	for _, project := range existing.Result {
		byName[strings.ToLower(project.Name)] = project
	}

	plan := &ProjectPlan{}
	plan.Columns = spreadsheet.ProjectColumns(language)

	// The rows the file itself could not be read for belong in the preview: they
	// are rows of somebody's file, and leaving them out would show 62 rows for a
	// file that has 64.
	for _, problem := range problems {
		plan.add(SheetRow{Number: problem.Number, Problem: problem.Reason,
			Code: problem.Code, Values: problem.Values})
	}

	for _, row := range rows {
		cells := projectCells(language, row)

		if err := checkProjectStatus(language, row.Status); err != nil {
			plan.add(problemRow(row.Number, cells, err))

			continue
		}

		planned := plannedProject{row: row}

		if found := byName[strings.ToLower(row.Name)]; found != nil {
			planned.existingID = found.ID
			planned.existingStatus = found.Status
		}

		// Archiving is only allowed from "completed", and that is about the status
		// the project has now - so a row asking for it has to be refused here
		// rather than at the write, where it would be a promise the preview had
		// already made.
		if err := checkArchiving(language, planned); err != nil {
			plan.add(problemRow(row.Number, cells, err))

			continue
		}

		plan.writable = append(plan.writable, planned)

		plan.add(SheetRow{Number: row.Number, Cells: cells})
	}

	plan.sorted()

	return plan, nil
}

// checkProjectStatus keeps an unknown status out of the write, where it would
// otherwise arrive as a validation error naming a field rather than a row.
func checkProjectStatus(language, status string) error {
	switch status {
	case "", model.ProjectStatusActive, model.ProjectStatusArchived,
		model.ProjectStatusCompleted:
		return nil
	}

	// The three allowed words in the language the file was read in, because the
	// preview beside this complaint writes the status column in that language too -
	// being told to use "archived" while looking at a column of "archiviert" is
	// being told to use a word the importer would then have to un-translate.
	return spreadsheet.Problemf("notAStatus", "%q is not a status; use %s, %s or %s",
		status,
		spreadsheet.Translate(language, model.ProjectStatusActive),
		spreadsheet.Translate(language, model.ProjectStatusArchived),
		spreadsheet.Translate(language, model.ProjectStatusCompleted))
}

// checkArchiving applies the archiving rule to a planned row.
//
// Only where the row would actually change the status. Without that, exporting an
// archived project and importing the file again was refused: the row says
// "archived", the project is already archived, and the rule reads a status that is
// not "completed" and says no - to a change that was not being asked for.
func checkArchiving(language string, planned plannedProject) error {
	if planned.existingID == 0 || !planned.changesStatus() {
		return nil
	}

	if planned.row.Status != model.ProjectStatusArchived {
		return nil
	}

	if planned.existingStatus != model.ProjectStatusCompleted {
		return spreadsheet.Problemf("archiveNeedsCompleted",
			"a project can only be archived once its status is %q; %q is %q",
			spreadsheet.Translate(language, model.ProjectStatusCompleted),
			planned.row.Name,
			spreadsheet.Translate(language, planned.existingStatus))
	}

	return nil
}

// changesStatus reports whether the row asks for a different status than the
// project already has.
func (p plannedProject) changesStatus() bool {
	return p.row.Status != "" && p.row.Status != p.existingStatus
}

func projectCells(language string, row spreadsheet.ProjectRow) []string {
	end := ""
	if !row.EndDate.IsZero() {
		end = row.EndDate.Format("2006-01-02")
	}

	start := ""
	if !row.StartDate.IsZero() {
		start = row.StartDate.Format("2006-01-02")
	}

	return []string{
		row.Name, row.Description, start, end,
		spreadsheet.Translate(language, row.Status),
	}
}

// ApplyProjects writes a planned file.
//
// Row by row through the project service, because that is where the rules live and
// these repositories have no batch write. That makes a connection lost half way
// through leave some rows written - which is survivable here in a way it is not for
// time entries: the same file imported again matches what the first attempt created
// by name and updates it, so re-importing finishes the job rather than doubling it.
// The count of what was written is returned either way, so nobody has to guess
// where it stopped.
func (s *ProjectWorkbookService) ApplyProjects(
	ctx context.Context,
	plan *ProjectPlan,
	actor *model.User,
) (int, error) {
	if actor == nil {
		return 0, apperror.InvalidFields("actor")
	}

	if plan == nil {
		return 0, apperror.Invalidf("there is nothing to import").WithCode("importEmpty")
	}

	if err := refuseRejected(&plan.SheetPlan); err != nil {
		return 0, err
	}

	written := 0

	for _, planned := range plan.writable {
		if err := s.write(ctx, planned, actor); err != nil {
			return written, apperror.Conflictf("row %d: %v; %d rows were written before it",
				planned.row.Number, err, written).
				WithCode("importStoppedAtRow", planned.row.Number, written)
		}

		written++
	}

	return written, nil
}

func (s *ProjectWorkbookService) write(
	ctx context.Context,
	planned plannedProject,
	actor *model.User,
) error {
	row := planned.row

	description := &row.Description
	if row.Description == "" {
		description = nil
	}

	var end *time.Time
	if !row.EndDate.IsZero() {
		end = &row.EndDate
	}

	if planned.existingID != 0 {
		name := row.Name

		// The status only where it changes. Sending the one it already has walks
		// into the archiving rule, which reads the current status and refuses a
		// change nobody asked for.
		var status *string
		if planned.changesStatus() {
			asked := row.Status
			status = &asked
		}

		_, err := s.projects.UpdateProject(ctx, command.UpdateProjectCommand{
			ID: planned.existingID, Name: &name, Description: description,
			StartDate: &row.StartDate, EndDate: end, Status: status,
			ActorID: actor.ID,
		})

		return err
	}

	// It belongs to whoever imported it, which is the only answer: a project is one
	// person's, and no cell can hand it to somebody else.
	owner := actor.ID

	_, err := s.projects.CreateProject(ctx, command.CreateProjectCommand{
		Name: row.Name, Description: description, StartDate: row.StartDate,
		EndDate: end, Status: row.Status, OwnerID: &owner,
	})

	return err
}
