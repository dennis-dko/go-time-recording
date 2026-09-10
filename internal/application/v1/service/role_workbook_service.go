package service

import (
	"context"
	"sort"
	"strings"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
	"github.com/dennis-dko/go-time-recording/internal/pkg/spreadsheet"
)

// RoleWorkbookService moves roles in and out as a spreadsheet.
//
// The one table left where a spreadsheet is genuinely the better tool. A role is a
// name and thirty tick boxes, and comparing four roles across thirty rights is a
// grid - which is what a reviewer actually wants to look at, and what the role
// editor, one role at a time, cannot show.
//
// API tokens and passkeys deliberately have no such pair, and that is not an
// oversight to be corrected later: a token's secret exists once, at creation, and
// is never retrievable, so an exported one would be a column of useless hashes and
// an imported one a token nobody can use. A passkey is bound to the device that
// holds it and cannot be moved by copying a row.
type RoleWorkbookService struct {
	roles repository.RoleRepository
	admin *RoleApplicationService
}

// NewRoleWorkbookService writes through the role service, so an imported role
// is held to the rules the role editor is.
func NewRoleWorkbookService(
	roles repository.RoleRepository,
	admin *RoleApplicationService,
) *RoleWorkbookService {
	return &RoleWorkbookService{roles: roles, admin: admin}
}

// Export writes the roles as a workbook, a column per right.
func (s *RoleWorkbookService) Export(ctx context.Context, language string) ([]byte, error) {
	all, err := s.roles.GetAll(ctx)
	if err != nil {
		return nil, err
	}

	rows := make([]spreadsheet.RoleRow, 0, len(all))

	for _, role := range all {
		rows = append(rows, spreadsheet.RoleRow{
			Name:        role.Name,
			Description: role.Description,
			Granted:     role.Permissions,
			System:      role.IsSystem,
		})
	}

	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })

	return spreadsheet.WriteRoles(language, model.AllPermissions(), rows)
}

// plannedRole is one row, resolved to the role it changes or creates.
type plannedRole struct {
	row spreadsheet.RoleRow

	// id is zero for a role that does not exist yet.
	id uint
}

// RolePlan is a file of roles, understood.
type RolePlan struct {
	SheetPlan

	writable []plannedRole
}

// PlanRoles works out what a file of roles would do, without writing.
//
// Matched on the name, the way the projects import is: a role is its name
// everywhere else in this application - accounts point at one, the directory
// configuration names one - so the name is the only thing a person filling in a
// spreadsheet can be expected to match on.
func (s *RoleWorkbookService) PlanRoles(
	ctx context.Context,
	language string,
	rows []spreadsheet.RoleRow,
	problems []spreadsheet.RowError,
) (*RolePlan, error) {
	existing, err := s.roles.GetAll(ctx)
	if err != nil {
		return nil, err
	}

	byName := make(map[string]*model.Role, len(existing))
	for _, role := range existing {
		byName[strings.ToLower(role.Name)] = role
	}

	permissions := model.AllPermissions()

	plan := &RolePlan{}
	plan.Columns = spreadsheet.RoleColumns(language, permissions)

	for _, problem := range problems {
		plan.add(SheetRow{Number: problem.Number, Problem: problem.Reason,
			Code: problem.Code, Values: problem.Values})
	}

	for _, row := range rows {
		cells := roleCells(language, permissions, row)
		found := byName[strings.ToLower(row.Name)]

		// A system role is what keeps an installation administrable, and its rights
		// are fixed for the reasons UpdateRole gives. Letting a spreadsheet be the
		// one way round that would make the file the widest door into the very
		// thing the form refuses.
		if found != nil && found.IsSystem && !sameRights(row.Granted, found.Permissions) {
			plan.add(problemRow(row.Number, cells, spreadsheet.Problemf("systemRole",
				"%q is a system role, and its permissions cannot be changed", found.Name)))

			continue
		}

		if found == nil && len(row.Granted) == 0 {
			plan.add(problemRow(row.Number, cells, spreadsheet.Problemf("roleGrantsNothing",
				"%q would be created granting nothing, so nobody holding it could "+
					"open a single screen", row.Name)))

			continue
		}

		planned := plannedRole{row: row}
		if found != nil {
			planned.id = found.ID
		}

		plan.writable = append(plan.writable, planned)
		plan.add(SheetRow{Number: row.Number, Cells: cells})
	}

	plan.sorted()

	return plan, nil
}

func roleCells(language string, permissions []string, row spreadsheet.RoleRow) []string {
	held := make(map[string]bool, len(row.Granted))
	for _, permission := range row.Granted {
		held[permission] = true
	}

	cells := []string{
		row.Name,
		row.Description,
		spreadsheet.Translate(language, yesNo(row.System)),
	}

	for _, permission := range permissions {
		cells = append(cells, spreadsheet.Translate(language, yesNo(held[permission])))
	}

	return cells
}

// yesNo is the word for a tick, in English, which is what Translate takes.
func yesNo(on bool) string {
	if on {
		return "yes"
	}

	return "no"
}

// ApplyRoles writes a planned file of roles.
//
// Through the application service rather than the repository, so a role written
// from a spreadsheet passes exactly the checks the form does: a name that is
// already taken, a permission the application does not enforce, a system role whose
// rights may not move.
func (s *RoleWorkbookService) ApplyRoles(ctx context.Context, plan *RolePlan) (int, error) {
	if plan == nil {
		return 0, apperror.Invalidf("there is nothing to import").WithCode("importEmpty")
	}

	if err := refuseRejected(&plan.SheetPlan); err != nil {
		return 0, err
	}

	written := 0

	for _, planned := range plan.writable {
		var err error

		if planned.id == 0 {
			_, err = s.admin.CreateRole(ctx,
				planned.row.Name, planned.row.Description, planned.row.Granted)
		} else {
			description := planned.row.Description
			_, err = s.admin.UpdateRole(ctx, planned.id, nil, &description, planned.row.Granted)
		}

		if err != nil {
			return written, apperror.Conflictf("row %d: %v; %d rows were written before it",
				planned.row.Number, err, written).
				WithCode("importStoppedAtRow", planned.row.Number, written)
		}

		written++
	}

	return written, nil
}
