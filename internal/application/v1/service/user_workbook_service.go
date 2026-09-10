package service

import (
	"context"
	"sort"
	"strings"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/query"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
	"github.com/dennis-dko/go-time-recording/internal/pkg/spreadsheet"
)

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

// NewUserWorkbookService writes through the account service, so an imported
// change passes the checks a correction on screen does.
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
		plan.add(SheetRow{Number: problem.Number, Problem: problem.Reason,
			Code: problem.Code, Values: problem.Values})
	}

	for _, row := range rows {
		cells := userCells(language, row)

		person, found := byEmail[row.Email]
		if !found {
			plan.add(problemRow(row.Number, cells, spreadsheet.Problemf("noSuchAccount",
				"there is no account for %q; this import changes accounts and does "+
					"not create them", row.Email)))

			continue
		}

		if role := strings.TrimSpace(row.Role); role != "" && !known[strings.ToLower(role)] {
			plan.add(problemRow(row.Number, cells,
				spreadsheet.Problemf("noSuchRole", "%q is not a role", role)))

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
