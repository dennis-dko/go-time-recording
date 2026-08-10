package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/common"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/query"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
	"github.com/dennis-dko/go-time-recording/internal/pkg/spreadsheet"
)

// SheetRow is one row of a file as a preview shows it.
//
// Cells as text, in the order of the sheet's own columns. The time-entry import
// has a typed response of its own, from when it was the only one; projects and
// people share this because their previews differ only in what the columns are
// called, and three preview tables in the interface to show the same three things
// - what a row would do, and why it cannot - would be two too many.
type SheetRow struct {
	// Number is the line in the spreadsheet, so somebody can go and look. The
	// heading is row 1.
	Number int

	Cells []string

	// Problem is empty for a row that would be written. In English, like every
	// other message the server produces: what is wrong with row 47 of somebody's
	// file is not a fixed set of reasons that code could translate.
	Problem string
}

// SheetPlan is a whole file, understood.
type SheetPlan struct {
	// Columns are the headings the preview shows, already in the reader's
	// language, so the preview and the file they exported say the same words.
	Columns []string

	Rows []SheetRow

	Writable int
	Rejected int
}

// Ready reports whether the whole file can be written.
func (p *SheetPlan) Ready() bool { return p.Rejected == 0 && p.Writable > 0 }

func (p *SheetPlan) add(row SheetRow) {
	if row.Problem == "" {
		p.Writable++
	} else {
		p.Rejected++
	}

	p.Rows = append(p.Rows, row)
}

// sorted orders a plan's rows by their line in the file.
//
// The unreadable rows are collected separately from the readable ones, so without
// this the preview would list row 47 above row 3 and nobody could find anything in
// it.
func (p *SheetPlan) sorted() {
	sort.SliceStable(p.Rows, func(i, j int) bool { return p.Rows[i].Number < p.Rows[j].Number })
}

// refuseRejected is the answer to applying a plan that has rows in it that cannot
// be written.
//
// Refused as a whole rather than partly: somebody who has been shown "3 of 64
// rows are wrong" and presses import again means "I fixed them", not "do the other
// 61 and let me guess which".
func refuseRejected(plan *SheetPlan) error {
	if plan == nil || len(plan.Rows) == 0 {
		return apperror.Invalidf("there is nothing to import").WithCode("importEmpty")
	}

	if plan.Rejected > 0 {
		return apperror.Conflictf(
			"%d of %d rows cannot be imported; nothing was written",
			plan.Rejected, len(plan.Rows)).
			WithCode("importHasRejectedRows", plan.Rejected, len(plan.Rows))
	}

	return nil
}

// ---------------------------------------------------------------- projects

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

// NewProjectWorkbookService creates new instance.
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
			Category:  project.OwnerID != nil,
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
// mayShare says whether the actor may touch shared projects at all; without it
// every row has to be a private category of their own. Checked here rather than
// left to the write, because a preview that promised rows the write would refuse
// would be worse than no preview.
func (s *ProjectWorkbookService) PlanProjects(
	ctx context.Context,
	language string,
	rows []spreadsheet.ProjectRow,
	problems []spreadsheet.RowError,
	actor *model.User,
	mayShare bool,
) (*ProjectPlan, error) {
	if actor == nil {
		return nil, apperror.InvalidFields("actor")
	}

	existing, err := s.projects.ListProjects(ctx, query.ListProjectsQuery{ViewerID: actor.ID})
	if err != nil {
		return nil, err
	}

	// Two indexes, because a private category and a shared project may have the
	// same name without being the same thing.
	shared := map[string]*common.ProjectResult{}
	own := map[string]*common.ProjectResult{}

	for _, project := range existing.Result {
		if project.OwnerID != nil {
			own[strings.ToLower(project.Name)] = project
		} else {
			shared[strings.ToLower(project.Name)] = project
		}
	}

	plan := &ProjectPlan{}
	plan.Columns = spreadsheet.ProjectColumns(language)

	// The rows the file itself could not be read for belong in the preview: they
	// are rows of somebody's file, and leaving them out would show 62 rows for a
	// file that has 64.
	for _, problem := range problems {
		plan.add(SheetRow{Number: problem.Number, Problem: problem.Reason})
	}

	for _, row := range rows {
		cells := projectCells(language, row)

		if err := checkProjectStatus(row.Status); err != nil {
			plan.add(SheetRow{Number: row.Number, Cells: cells, Problem: err.Error()})

			continue
		}

		if !row.Category && !mayShare {
			plan.add(SheetRow{Number: row.Number, Cells: cells, Problem: "this row is a " +
				"shared project, and you may only keep private categories; mark it as " +
				"a category or ask somebody who may"})

			continue
		}

		index := shared
		if row.Category {
			index = own
		}

		planned := plannedProject{row: row}

		if found := index[strings.ToLower(row.Name)]; found != nil {
			planned.existingID = found.ID
			planned.existingStatus = found.Status
		}

		// Archiving is only allowed from "completed", and that is about the status
		// the project has now - so a row asking for it has to be refused here
		// rather than at the write, where it would be a promise the preview had
		// already made.
		if err := checkArchiving(planned); err != nil {
			plan.add(SheetRow{Number: row.Number, Cells: cells, Problem: err.Error()})

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
func checkProjectStatus(status string) error {
	switch status {
	case "", model.ProjectStatusActive, model.ProjectStatusArchived,
		model.ProjectStatusCompleted:
		return nil
	}

	return fmt.Errorf("%q is not a status; use %s, %s or %s", status,
		model.ProjectStatusActive, model.ProjectStatusArchived, model.ProjectStatusCompleted)
}

// checkArchiving applies the archiving rule to a planned row.
//
// Only where the row would actually change the status. Without that, exporting an
// archived project and importing the file again was refused: the row says
// "archived", the project is already archived, and the rule reads a status that is
// not "completed" and says no - to a change that was not being asked for.
func checkArchiving(planned plannedProject) error {
	if planned.existingID == 0 || !planned.changesStatus() {
		return nil
	}

	if planned.row.Status != model.ProjectStatusArchived {
		return nil
	}

	if planned.existingStatus != model.ProjectStatusCompleted {
		return fmt.Errorf("a project can only be archived once its status is %q; %q is %q",
			model.ProjectStatusCompleted, planned.row.Name, planned.existingStatus)
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

	category := "no"
	if row.Category {
		category = "yes"
	}

	return []string{
		row.Name, row.Description, start, end,
		spreadsheet.Translate(language, row.Status),
		spreadsheet.Translate(language, category),
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

	// A category belongs to whoever imported it. Private is private: it cannot be
	// handed to somebody else by filling in a cell.
	var owner *uint
	if row.Category {
		id := actor.ID
		owner = &id
	}

	_, err := s.projects.CreateProject(ctx, command.CreateProjectCommand{
		Name: row.Name, Description: description, StartDate: row.StartDate,
		EndDate: end, Status: row.Status, OwnerID: owner,
	})

	return err
}

// ------------------------------------------------------------------- people

// UserWorkbookService moves accounts in and out as a spreadsheet.
//
// Import changes existing accounts and does not create them, matched on the mail
// address. Creating one needs a password, and a password that arrived in a
// spreadsheet is a password that has been mailed around - while one generated here
// would have to be read back out of the file to be of any use to anybody. What this
// is actually wanted for works without either: giving forty people a new daily
// target, or moving a department to another role.
type UserWorkbookService struct {
	users    repository.UserRepository
	roles    repository.RoleRepository
	accounts *UserApplicationService
}

// NewUserWorkbookService creates new instance.
func NewUserWorkbookService(
	users repository.UserRepository,
	roles repository.RoleRepository,
	accounts *UserApplicationService,
) *UserWorkbookService {
	return &UserWorkbookService{users: users, roles: roles, accounts: accounts}
}

// Export writes the accounts as a workbook.
func (s *UserWorkbookService) Export(ctx context.Context, language string) ([]byte, error) {
	listed, err := s.accounts.ListUsers(ctx, query.ListUsersQuery{})
	if err != nil {
		return nil, err
	}

	rows := make([]spreadsheet.UserRow, 0, len(listed.Result))

	for _, user := range listed.Result {
		rows = append(rows, spreadsheet.UserRow{
			Name:      user.Name,
			Email:     user.Email,
			Role:      user.Role,
			Directory: user.IsExternal,
		})
	}

	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Email < rows[j].Email })

	return spreadsheet.WriteUsers(language, rows)
}

// plannedUser is one row, resolved to the account it changes.
type plannedUser struct {
	row spreadsheet.UserRow
	id  uint
}

// UserPlan is a file of accounts, understood.
type UserPlan struct {
	SheetPlan

	writable []plannedUser
}

// PlanUsers works out what a file of accounts would do, without writing.
func (s *UserWorkbookService) PlanUsers(
	ctx context.Context,
	language string,
	rows []spreadsheet.UserRow,
	problems []spreadsheet.RowError,
) (*UserPlan, error) {
	people, err := s.users.GetAll(ctx)
	if err != nil {
		return nil, err
	}

	byEmail := make(map[string]*model.User, len(people))
	for _, person := range people {
		byEmail[strings.ToLower(person.Email)] = person
	}

	known, err := s.roleNames(ctx)
	if err != nil {
		return nil, err
	}

	plan := &UserPlan{}
	plan.Columns = spreadsheet.UserColumns(language)

	for _, problem := range problems {
		plan.add(SheetRow{Number: problem.Number, Problem: problem.Reason})
	}

	for _, row := range rows {
		cells := userCells(language, row)

		person, found := byEmail[row.Email]
		if !found {
			plan.add(SheetRow{Number: row.Number, Cells: cells, Problem: fmt.Sprintf(
				"there is no account for %q; this import changes accounts and does "+
					"not create them", row.Email)})

			continue
		}

		if role := strings.TrimSpace(row.Role); role != "" && !known[strings.ToLower(role)] {
			plan.add(SheetRow{Number: row.Number, Cells: cells, Problem: fmt.Sprintf(
				"%q is not a role", role)})

			continue
		}

		plan.writable = append(plan.writable, plannedUser{row: row, id: person.ID})
		plan.add(SheetRow{Number: row.Number, Cells: cells})
	}

	plan.sorted()

	return plan, nil
}

func (s *UserWorkbookService) roleNames(ctx context.Context) (map[string]bool, error) {
	roles, err := s.roles.GetAll(ctx)
	if err != nil {
		return nil, err
	}

	known := make(map[string]bool, len(roles))
	for _, role := range roles {
		known[strings.ToLower(role.Name)] = true
	}

	return known, nil
}

func userCells(language string, row spreadsheet.UserRow) []string {
	directory := "no"
	if row.Directory {
		directory = "yes"
	}

	return []string{
		row.Name, row.Email, row.Role, spreadsheet.Translate(language, directory),
	}
}

// ApplyUsers writes a planned file of accounts.
//
// Through UpdateUser, so the built-in administrator keeps a role that can still
// administer and every account still passes validation - the same checks the form
// goes through. Setting a field to the value it already has is what makes this safe
// to run twice, so a run that stopped part way is finished by running it again.
//
// The name and the role, and nothing else. The daily target, the ceiling and the time
// zone are time figures: they belong to the person they are about, who sets them
// under My account. This wrote all three, which made a spreadsheet the widest way
// into the very settings a single right was supposed to guard.
func (s *UserWorkbookService) ApplyUsers(ctx context.Context, plan *UserPlan) (int, error) {
	if plan == nil {
		return 0, apperror.Invalidf("there is nothing to import").WithCode("importEmpty")
	}

	if err := refuseRejected(&plan.SheetPlan); err != nil {
		return 0, err
	}

	written := 0

	for _, planned := range plan.writable {
		cmd := command.UpdateUserCommand{ID: planned.id}

		// Only the cells that were filled in. An empty cell means "leave this as
		// it is": a spreadsheet with a column somebody deleted must not blank out
		// forty people's working times.
		if name := strings.TrimSpace(planned.row.Name); name != "" {
			cmd.Name = &name
		}

		if role := strings.TrimSpace(planned.row.Role); role != "" {
			cmd.Role = &role
		}

		if _, err := s.accounts.UpdateUser(ctx, cmd); err != nil {
			return written, apperror.Conflictf("row %d: %v; %d rows were written before it",
				planned.row.Number, err, written).
				WithCode("importStoppedAtRow", planned.row.Number, written)
		}

		written++
	}

	return written, nil
}
