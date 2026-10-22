package service

import (
	"cmp"
	"context"
	"slices"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// TeamService answers the one question that reaches across accounts: may this
// caller act for that account, and with which right.
//
// Three conditions, all of them every time. The installation has managers - a
// switch under Settings, shipped off, so an installation that never chose them
// behaves exactly as before they existed. The caller's role holds the team right
// asked about. And an administrator assigned that account to the caller. Asked
// afresh on every request, like every other right here, so a switch turned off or
// an assignment taken back is refused on the very next call.
//
// A team right never reaches the caller's own account: that is what the own rights
// are for, and an answer of yes there would let a role with no own rights read
// itself through a team one.
type TeamService struct {
	assignments repository.ManagedAccountRepository
	settings    repository.SettingsRepository
	users       repository.UserRepository
	roles       repository.RoleRepository
	projects    repository.ProjectRepository
}

// NewTeamService needs the assignments, the switch, the accounts and roles to name
// people and read their rights, and the projects to say whose a project is.
func NewTeamService(assignments repository.ManagedAccountRepository, settings repository.SettingsRepository,
	users repository.UserRepository, roles repository.RoleRepository,
	projects repository.ProjectRepository) *TeamService {
	return &TeamService{assignments: assignments, settings: settings, users: users, roles: roles, projects: projects}
}

// TeamMember is somebody on the other end of an assignment, and the team rights
// that reach across it - the caller's over them, or theirs over the caller.
type TeamMember struct {
	ID     uint
	Name   string
	Rights []string
}

// Enabled says whether the installation has managers.
func (s *TeamService) Enabled(ctx context.Context) (bool, error) {
	value, err := s.settings.Get(ctx, model.SettingManagers)

	return value == "true", err
}

// SetEnabled switches managers on or off. Off keeps every assignment, so turning
// them on again restores what an administrator had set up rather than starting over.
func (s *TeamService) SetEnabled(ctx context.Context, on bool) error {
	value := ""
	if on {
		value = "true"
	}

	return s.settings.Set(ctx, model.SettingManagers, value)
}

// Reaches says whether the caller may act for an account with a team right.
func (s *TeamService) Reaches(ctx context.Context, caller *Principal, userID uint, permission string) (bool, error) {
	if caller == nil || caller.User == nil || caller.User.ID == userID ||
		!slices.Contains(model.TeamPermissions(), permission) || !caller.Can(permission) {
		return false, nil
	}

	enabled, err := s.Enabled(ctx)
	if err != nil || !enabled {
		return false, err
	}

	members, err := s.assignments.MembersOf(ctx, caller.User.ID)
	if err != nil {
		return false, err
	}

	return slices.Contains(members, userID), nil
}

// Member is the account behind userID where the caller reaches it with one of the
// team rights named, and nil where they reach it with none - Reaches, answered with
// the account, so that its days can be read in its own zone.
func (s *TeamService) Member(ctx context.Context, caller *Principal, userID uint,
	rights ...string) (*model.User, error) {
	for _, right := range rights {
		reached, err := s.Reaches(ctx, caller, userID, right)
		if err != nil {
			return nil, err
		}

		if reached {
			return s.users.GetByID(ctx, userID)
		}
	}

	return nil, nil
}

// ProjectActor is whose name a request about a project is made in: its owner's,
// where the caller looks after them with one of the team rights named, and the
// caller's own otherwise.
//
// The project services ask whether a project is the actor's, and answer "not
// found" where it is not. Handing them the owner is what lets a manager read or
// change a project of somebody assigned to them, and handing them the caller
// everywhere else keeps every other project exactly as absent as it was before
// managers existed - including one that does not exist at all, which is why a
// project this cannot find is not an error here but left for the service to say.
func (s *TeamService) ProjectActor(ctx context.Context, caller *Principal, projectID uint,
	rights ...string) (uint, error) {
	project, err := s.projects.GetByID(ctx, projectID)
	if err != nil {
		if detail, ok := apperror.Detail(err); ok && detail.Kind == apperror.KindNotFound {
			return caller.User.ID, nil
		}

		return 0, err
	}

	if project.OwnerID == nil {
		return caller.User.ID, nil
	}

	owner, err := s.Member(ctx, caller, *project.OwnerID, rights...)
	if err != nil {
		return 0, err
	}

	if owner != nil {
		return owner.ID, nil
	}

	return caller.User.ID, nil
}

// Team lists the accounts the caller looks after, with the rights the caller's role
// gives over them, ordered by name - and nobody while the installation has no
// managers or the caller's role holds no team right.
func (s *TeamService) Team(ctx context.Context, caller *Principal) ([]TeamMember, error) {
	rights := teamRightsIn(caller.Permissions)

	enabled, err := s.Enabled(ctx)
	if err != nil || !enabled || len(rights) == 0 {
		return []TeamMember{}, err
	}

	members, err := s.assignments.MembersOf(ctx, caller.User.ID)
	if err != nil {
		return nil, err
	}

	return s.named(ctx, members, func(context.Context, *model.User) ([]string, error) { return rights, nil })
}

// ManagersOf lists who looks after an account and with which rights, ordered by
// name - what the account's owner is shown, so they know who may see their time.
// Nobody while the installation has no managers, because nobody then can.
func (s *TeamService) ManagersOf(ctx context.Context, userID uint) ([]TeamMember, error) {
	enabled, err := s.Enabled(ctx)
	if err != nil || !enabled {
		return []TeamMember{}, err
	}

	managers, err := s.assignments.ManagersOf(ctx, userID)
	if err != nil {
		return nil, err
	}

	return s.named(ctx, managers, s.rightsOf)
}

// ManagerIDsOf is who an administrator assigned to look after an account, as it
// is stored - switched on or not, because it is what switching on would reach.
func (s *TeamService) ManagerIDsOf(ctx context.Context, userID uint) ([]uint, error) {
	return s.assignments.ManagersOf(ctx, userID)
}

// SetManagers assigns an account its managers, replacing those it had.
//
// Each has to be somebody whose role gives a team right, because an assignment to
// anybody else would be a promise nothing keeps - and nobody manages their own
// account, which is what the own rights are for.
func (s *TeamService) SetManagers(ctx context.Context, userID uint, managerIDs []uint) error {
	if _, err := s.users.GetByID(ctx, userID); err != nil {
		return err
	}

	managers := slices.Clone(managerIDs)
	slices.Sort(managers)
	managers = slices.Compact(managers)

	for _, managerID := range managers {
		if managerID == userID {
			return apperror.Invalidf("an account cannot be its own manager").WithCode("selfManaged")
		}

		manager, err := s.users.GetByID(ctx, managerID)
		if err != nil {
			return err
		}

		rights, err := s.rightsOf(ctx, manager)
		if err != nil {
			return err
		}

		if len(rights) == 0 {
			return apperror.Invalidf("%s holds no role that looks after other accounts", manager.Name).
				WithCode("notAManager", manager.Name)
		}
	}

	return s.assignments.SetManagers(ctx, userID, managers)
}

// rightsOf is the team rights an account's role gives it.
func (s *TeamService) rightsOf(ctx context.Context, user *model.User) ([]string, error) {
	if user.RoleID == 0 {
		return nil, nil
	}

	role, err := s.roles.GetByID(ctx, user.RoleID)
	if err != nil {
		if detail, ok := apperror.Detail(err); ok && detail.Kind == apperror.KindNotFound {
			return nil, nil
		}

		return nil, err
	}

	return teamRightsIn(role.Permissions), nil
}

// named reads the accounts behind ids with their rights, ordered by name; an id
// whose account is gone is left out rather than shown as nobody.
func (s *TeamService) named(ctx context.Context, ids []uint,
	rights func(context.Context, *model.User) ([]string, error)) ([]TeamMember, error) {
	named := make([]TeamMember, 0, len(ids))

	for _, id := range ids {
		user, err := s.users.GetByID(ctx, id)
		if err != nil {
			if detail, ok := apperror.Detail(err); ok && detail.Kind == apperror.KindNotFound {
				continue
			}

			return nil, err
		}

		held, err := rights(ctx, user)
		if err != nil {
			return nil, err
		}

		named = append(named, TeamMember{ID: user.ID, Name: user.Name, Rights: held})
	}

	slices.SortFunc(named, func(a, b TeamMember) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.ID, b.ID))
	})

	return named, nil
}

// teamRightsIn is the team rights among a set of permissions, in their own order.
func teamRightsIn(permissions []string) []string {
	rights := []string{}

	for _, right := range model.TeamPermissions() {
		if slices.Contains(permissions, right) {
			rights = append(rights, right)
		}
	}

	return rights
}
