package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
	"github.com/dennis-dko/go-time-recording/internal/pkg/spreadsheet"
)

// WorkbookService moves time entries in and out as a spreadsheet.
//
// Export is the easy half. Import writes many rows from a file somebody assembled
// by hand, and two things follow from that.
//
// Every row goes through the rules the API enforces, by calling them rather than by
// restating them: the same validateTimesheet, the same daily budget, the same
// project visibility. An importer with its own idea of what is valid would be a
// second, quieter API - and the quiet one is the one nobody checks.
//
// And it is all or nothing. A file half-imported leaves somebody looking for which
// half, with no way to tell an entry that came from the file from one that was
// already there. So the plan is built first, complete, and only written if every
// row of it can be.
type WorkbookService struct {
	timesheets repository.TimesheetRepository
	users      repository.UserRepository
	projects   repository.ProjectRepository

	// The service that owns the rules, so they are called rather than copied.
	entries *TimesheetApplicationService
}

// NewWorkbookService creates new instance.
func NewWorkbookService(
	timesheets repository.TimesheetRepository,
	users repository.UserRepository,
	projects repository.ProjectRepository,
	entries *TimesheetApplicationService,
) *WorkbookService {
	return &WorkbookService{
		timesheets: timesheets, users: users, projects: projects, entries: entries,
	}
}

// Export writes the entries matching filter as a workbook.
//
// The filter is the caller's, already narrowed to what they may see: this does no
// scoping of its own, because doing it in two places is how the two come to
// disagree.
func (s *WorkbookService) Export(
	ctx context.Context,
	filter repository.TimesheetFilter,
	language string,
) ([]byte, error) {
	entries, err := s.timesheets.GetByFilter(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Oldest first, which is how a timesheet is read.
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Date.Equal(entries[j].Date) {
			return entries[i].ID < entries[j].ID
		}

		return entries[i].Date.Before(entries[j].Date)
	})

	names, err := s.userNames(ctx)
	if err != nil {
		return nil, err
	}

	projects, err := s.projectNames(ctx)
	if err != nil {
		return nil, err
	}

	rows := make([]spreadsheet.Row, 0, len(entries))

	for _, entry := range entries {
		row := spreadsheet.Row{
			Date:  entry.Date,
			User:  names[entry.UserID],
			Hours: entry.DurationHours,
		}

		if entry.HasProject() {
			row.Project = projects[*entry.ProjectID]
		}

		if entry.Description != nil {
			row.Description = *entry.Description
		}

		rows = append(rows, row)
	}

	// In the reader's language, the way the project and user exports already were.
	// This one called the untranslated writer, so a German screen produced a file
	// headed Date, User, Project, Hours - and the person who then edited it and
	// sent it back was the only one who could tell that the round trip still
	// worked, because the importer accepts both.
	return spreadsheet.WriteIn(language, rows)
}

// PlannedRow is one row of a file, as it would be written - or the reason it
// cannot be.
//
// Both are returned for every row, because the point of a preview is to show
// somebody the file they actually have: which rows are fine, which are not, and
// what is wrong with the ones that are not, all at once rather than one per
// attempt.
type PlannedRow struct {
	Number int

	Date        time.Time
	UserID      uint
	UserName    string
	ProjectID   *uint
	ProjectName string
	Hours       float64
	Description string

	// Problem is empty when the row can be written, and written in English
	// otherwise - which is what a log wants, and the fallback for a client that
	// cannot do better.
	Problem string

	// Code names which refusal it is and Values are what its sentence
	// interpolated, so the interface can say the same thing in the reader's
	// language.
	//
	// This was Problem alone, on the grounds that what is wrong with row 47 of
	// somebody's file is not a fixed set of reasons that code could translate.
	// It is - they are all written a few lines below - and the preview it lands in
	// puts every other column in the reader's language.
	Code   string
	Values []any
}

// Writable reports whether this row would be written.
func (r PlannedRow) Writable() bool { return r.Problem == "" }

// ImportPlan is a whole file, understood.
type ImportPlan struct {
	Rows []PlannedRow

	// Writable and Rejected count the rows either way, so the interface can say
	// "62 of 64" without walking the list.
	Writable int
	Rejected int
}

// Ready reports whether the whole file can be written.
func (p *ImportPlan) Ready() bool { return p.Rejected == 0 && p.Writable > 0 }

// Plan works out what a file would do, without writing anything.
//
// actor is who is importing. mayWriteAll says whether they may book for other
// people; without it every row has to be their own, which is checked here rather
// than left to the write - a preview that promised rows the write would refuse
// would be worse than no preview.
func (s *WorkbookService) Plan(
	ctx context.Context,
	rows []spreadsheet.Row,
	problems []spreadsheet.RowError,
	actor *model.User,
	mayWriteAll bool,
) (*ImportPlan, error) {
	if actor == nil {
		return nil, apperror.InvalidFields("actor")
	}

	plan := &ImportPlan{Rows: make([]PlannedRow, 0, len(rows)+len(problems))}

	// The rows the file itself could not be read for are part of the plan: they are
	// rows of somebody's file, and leaving them out would show a preview of 62 rows
	// for a file that has 64.
	for _, problem := range problems {
		plan.Rows = append(plan.Rows, PlannedRow{
			Number: problem.Number, Problem: problem.Reason,
			Code: problem.Code, Values: problem.Values,
		})
	}

	people, err := s.peopleByName(ctx)
	if err != nil {
		return nil, err
	}

	visibleProjects, err := s.projects.GetAll(ctx)
	if err != nil {
		return nil, err
	}

	// Hours already planned per person and day, so the daily ceiling counts the
	// file against itself. Without this, forty rows of two hours on one day would
	// each be checked against a database that knows nothing of the other
	// thirty-nine, and the import would put through a day the API would have
	// refused one entry at a time.
	planned := map[string]float64{}

	for _, row := range rows {
		plan.Rows = append(plan.Rows,
			s.planRow(ctx, row, actor, mayWriteAll, people, visibleProjects, planned))
	}

	// Back into the order the file has them in, so the preview reads like the file.
	sort.Slice(plan.Rows, func(i, j int) bool { return plan.Rows[i].Number < plan.Rows[j].Number })

	for _, row := range plan.Rows {
		if row.Writable() {
			plan.Writable++
		} else {
			plan.Rejected++
		}
	}

	return plan, nil
}

// planRow decides what one row would do.
func (s *WorkbookService) planRow(
	ctx context.Context,
	row spreadsheet.Row,
	actor *model.User,
	mayWriteAll bool,
	people map[string]*model.User,
	projects []*model.Project,
	planned map[string]float64,
) PlannedRow {
	out := PlannedRow{
		Number: row.Number, Date: row.Date, Hours: row.Hours,
		Description: strings.TrimSpace(row.Description),
	}

	refuse := func(code, format string, a ...any) PlannedRow {
		out.Problem = fmt.Sprintf(format, a...)
		out.Code, out.Values = code, a

		return out
	}

	// A rule that already names itself keeps its own code and values rather than
	// being given new ones here: it is the same refusal the form gives, and the
	// interface has a sentence for it already.
	refuseAs := func(err error) PlannedRow {
		out.Problem = err.Error()

		var named *apperror.Error
		if !errors.As(err, &named) {
			return out
		}

		out.Code, out.Values = named.Code, named.Values

		// A refusal that names columns rather than giving a reason. Its English
		// wording is "invalid field(s): durationHours", which is a database column
		// in any language; the interface names each one the way the form above it
		// does, and this is what lets it.
		if out.Code == "" && len(named.Fields) > 0 {
			out.Code = "invalidFields"
			out.Values = make([]any, 0, len(named.Fields))

			for _, field := range named.Fields {
				out.Values = append(out.Values, field)
			}
		}

		return out
	}

	// Whose entry it is. Blank means the person importing, which is what makes a
	// file of one's own hours the simplest possible case.
	target := actor

	if name := strings.TrimSpace(row.User); name != "" {
		found, ok := people[strings.ToLower(name)]
		if !ok {
			return refuse("noSuchUser", "there is no user called %q", name)
		}

		target = found
	}

	out.UserID, out.UserName = target.ID, target.Name

	if target.ID != actor.ID && !mayWriteAll {
		return refuse("notYourTime",
			"you may only import your own time, and this row is %s's", target.Name)
	}

	// The project, by name, and only one this person may book against.
	if name := strings.TrimSpace(row.Project); name != "" {
		project := findProject(projects, name)
		if project == nil {
			return refuse("noSuchProject", "there is no project called %q", name)
		}

		if !project.VisibleTo(target.ID) {
			// The same answer somebody would get asking for it directly: its
			// existence is not something to reveal by a different message.
			return refuse("noSuchProject", "there is no project called %q", name)
		}

		// The same condition the booking path applies: a finished project would
		// have its final figures moved by a late entry.
		if project.Status != model.ProjectStatusActive {
			return refuse("projectNotActive",
				"project %q is %s and no longer accepts time entries",
				project.Name, project.Status)
		}

		id := project.ID
		out.ProjectID, out.ProjectName = &id, project.Name
	}

	// The rules the API enforces, called rather than restated.
	description := descriptionOrNil(out.Description)

	if err := validateTimesheet(row.Date, row.Hours, description); err != nil {
		return refuseAs(err)
	}

	// The ceiling, counting what the file has already put on this day.
	key := fmt.Sprintf("%d|%s", target.ID, row.Date.Format(dayKey))

	if err := s.entries.checkDailyBudget(ctx, target.ID, row.Date,
		row.Hours+planned[key], 0); err != nil {
		return refuseAs(err)
	}

	planned[key] += row.Hours

	return out
}

// Apply writes a plan, or nothing at all.
//
// Refused outright if any row of it cannot be written: see the type comment for
// why a half-imported file is worse than a refused one. The plan has to be the one
// Plan produced for this file - it carries the resolved ids, so nothing is looked
// up twice and the preview cannot describe one thing and the write do another.
func (s *WorkbookService) Apply(ctx context.Context, plan *ImportPlan) (int, error) {
	if plan == nil || len(plan.Rows) == 0 {
		return 0, apperror.Invalidf("there is nothing to import").WithCode("importEmpty")
	}

	if plan.Rejected > 0 {
		return 0, apperror.Conflictf(
			"%d of %d rows cannot be imported; nothing was written",
			plan.Rejected, len(plan.Rows)).
			WithCode("importHasRejectedRows", plan.Rejected, len(plan.Rows))
	}

	entries := make([]*model.Timesheet, 0, len(plan.Rows))

	for _, row := range plan.Rows {
		entries = append(entries, &model.Timesheet{
			UserID:        row.UserID,
			ProjectID:     row.ProjectID,
			Date:          row.Date,
			DurationHours: row.Hours,
			Description:   descriptionOrNil(row.Description),
		})
	}

	// One transaction, so a connection lost half way through leaves the database
	// as it was rather than describing a file nobody has.
	if err := s.timesheets.SaveMany(ctx, entries); err != nil {
		return 0, err
	}

	return len(entries), nil
}

// descriptionOrNil keeps an empty description out of the column, so a row with
// nothing in it stores nothing rather than an empty string.
func descriptionOrNil(text string) *string {
	if text == "" {
		return nil
	}

	return &text
}

// findProject resolves a project by name, case-insensitively: a spreadsheet is
// typed by a person, and "shared work" is the same project as "Shared work".
func findProject(projects []*model.Project, name string) *model.Project {
	for _, project := range projects {
		if strings.EqualFold(project.Name, name) {
			return project
		}
	}

	return nil
}

// userNames maps ids to names, for the export.
func (s *WorkbookService) userNames(ctx context.Context) (map[uint]string, error) {
	people, err := s.users.GetAll(ctx)
	if err != nil {
		return nil, err
	}

	names := make(map[uint]string, len(people))
	for _, person := range people {
		names[person.ID] = person.Name
	}

	return names, nil
}

// projectNames maps ids to names, for the export.
func (s *WorkbookService) projectNames(ctx context.Context) (map[uint]string, error) {
	projects, err := s.projects.GetAll(ctx)
	if err != nil {
		return nil, err
	}

	names := make(map[uint]string, len(projects))
	for _, project := range projects {
		names[project.ID] = project.Name
	}

	return names, nil
}

// peopleByName indexes accounts by the two things a spreadsheet can name them by.
//
// Both, because a file exported from here carries names and a file assembled by
// hand usually carries mail addresses - and a name is not unique, so the address is
// what settles it when two people are called the same thing. Lower-cased, since
// neither is case-sensitive to a person.
func (s *WorkbookService) peopleByName(ctx context.Context) (map[string]*model.User, error) {
	people, err := s.users.GetAll(ctx)
	if err != nil {
		return nil, err
	}

	index := make(map[string]*model.User, len(people)*2)

	for _, person := range people {
		// The address first and unconditionally: it is unique, so it can never be
		// the ambiguous one.
		index[strings.ToLower(person.Email)] = person
	}

	// Names second, and only where they are not already taken by an address and not
	// shared by two people. A name two accounts answer to is left out rather than
	// resolved to whichever came back first: importing somebody else's hours
	// against their colleague is not a mistake to make quietly.
	counts := map[string]int{}
	for _, person := range people {
		counts[strings.ToLower(person.Name)]++
	}

	for _, person := range people {
		key := strings.ToLower(person.Name)

		if counts[key] > 1 {
			continue
		}

		if _, taken := index[key]; !taken {
			index[key] = person
		}
	}

	return index, nil
}
