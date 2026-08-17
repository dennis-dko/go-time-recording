package service

import (
	"context"
	"slices"
	"strings"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

// RoleService administers roles and their permissions.
type RoleService interface {
	ListRoles(ctx context.Context) ([]*model.Role, error)
	GetRole(ctx context.Context, id uint) (*model.Role, error)
	CreateRole(ctx context.Context, name, description string, permissions []string) (*model.Role, error)
	UpdateRole(ctx context.Context, id uint, name, description *string, permissions []string) (*model.Role, error)
	DeleteRole(ctx context.Context, id uint) error
}

// RoleApplicationService application service for roles.
type RoleApplicationService struct {
	roles repository.RoleRepository
}

// NewRoleApplicationService creates new instance.
func NewRoleApplicationService(roles repository.RoleRepository) *RoleApplicationService {
	return &RoleApplicationService{roles: roles}
}

var _ RoleService = (*RoleApplicationService)(nil)

func (s *RoleApplicationService) ListRoles(ctx context.Context) ([]*model.Role, error) {
	return s.roles.GetAll(ctx)
}

func (s *RoleApplicationService) GetRole(ctx context.Context, id uint) (*model.Role, error) {
	if id == 0 {
		return nil, apperror.InvalidFields("id")
	}

	return s.roles.GetByID(ctx, id)
}

func (s *RoleApplicationService) CreateRole(
	ctx context.Context,
	name, description string,
	permissions []string,
) (*model.Role, error) {
	clean, err := validateRole(name, permissions)
	if err != nil {
		return nil, err
	}

	return s.roles.Save(ctx, &model.Role{
		Name:        strings.TrimSpace(name),
		Description: strings.TrimSpace(description),
		Permissions: clean,
	})
}

func (s *RoleApplicationService) UpdateRole(
	ctx context.Context,
	id uint,
	name, description *string,
	permissions []string,
) (*model.Role, error) {
	if id == 0 {
		return nil, apperror.InvalidFields("id")
	}

	role, err := s.roles.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// A system role may be described differently, and nothing else: not renamed, and
	// not given or taken a single permission.
	//
	// Narrowing was always refused - the application looks the role up by name, and
	// stripping it would leave the installation unadministrable. Widening was allowed,
	// on the reasoning that whoever may manage roles can reach anything anyway. That
	// reasoning does not survive the arrangement this application has now: the built-in
	// administrator exists to configure the installation and keep the accounts, and it
	// does not record time. A right added here would hand a working day to the one
	// account nobody chose, quietly, from the screen that administers roles.
	//
	// The way to let somebody both work here and administer is to give a person the
	// combined role. That is a decision about a colleague, which is the point.
	// Every role this application ships with, not only the administrator's.
	//
	// It was the administrator's alone at first, on the reasoning that the other
	// two are ordinary roles whose rights an installation may want to adjust. What
	// that misses is that they are also the answer to questions asked elsewhere:
	// the role every new account is given, the one the directory synchronisation
	// assigns, and the pair the interface names in its own words. A shipped role
	// renamed or emptied breaks those quietly, and the installation is left with a
	// role called something else granting something else under a name the
	// interface still translates.
	//
	// A role that should grant something different is a new role. Making one is a
	// minute's work and it says what it is.
	if role.IsDefault() {
		if name != nil && *name != role.Name {
			return nil, apperror.Conflictf("the shipped role %q cannot be renamed", role.Name).
				WithCode("systemRoleUnrenamable", role.Name)
		}

		if permissions != nil && !sameRights(permissions, role.Permissions) {
			return nil, apperror.Conflictf(
				"the permissions of the shipped role %q cannot be changed", role.Name).
				WithCode("systemRoleRightsFixed", role.Name)
		}

		// And what it says about itself. This was the one part still open, on the
		// reasoning that a description is only words - but these three are the
		// words the interface translates, keyed on the name. An installation that
		// edits one gets a description in one language that the interface then
		// overrides in another, which reads as the change not having been saved.
		//
		// Nothing about a shipped role is editable now, which is also what its
		// screen offers: it is shown rather than opened for changes.
		if description != nil && strings.TrimSpace(*description) != role.Description {
			return nil, apperror.Conflictf(
				"the description of the shipped role %q cannot be changed", role.Name).
				WithCode("systemRoleDescriptionFixed", role.Name)
		}
	}

	if name != nil {
		role.Name = strings.TrimSpace(*name)
	}

	if description != nil {
		role.Description = strings.TrimSpace(*description)
	}

	if permissions != nil {
		clean, permErr := validateRole(role.Name, permissions)
		if permErr != nil {
			return nil, permErr
		}

		role.Permissions = clean
	}

	if role.Name == "" {
		return nil, apperror.InvalidFields("name")
	}

	return s.roles.Update(ctx, role)
}

func (s *RoleApplicationService) DeleteRole(ctx context.Context, id uint) error {
	if id == 0 {
		return apperror.InvalidFields("id")
	}

	role, err := s.roles.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if role.IsSystem {
		return apperror.Conflictf("the system role %q cannot be deleted", role.Name).
			WithCode("systemRoleUndeletable", role.Name)
	}

	// The other shipped roles go the same way, and for a plainer reason than the
	// admin role's.
	//
	// They are ordinary roles - what they grant is this installation's business
	// and can be edited - but they are also the furniture: the role every account
	// is given on arrival, and the one the directory synchronisation assigns by
	// default. Deleting one leaves those without an answer, and putting it back by
	// hand means reproducing a list of permissions exactly from memory.
	//
	// Undeletable rather than untouchable, which is the difference from IsSystem:
	// an installation may still decide what its own users may do.
	if role.IsDefault() {
		return apperror.Conflictf("%q is one of the roles this application ships "+
			"with and cannot be deleted", role.Name).
			WithCode("defaultRoleUndeletable", role.Name)
	}

	inUse, err := s.roles.CountUsers(ctx, id)
	if err != nil {
		return err
	}

	if inUse > 0 {
		return apperror.Conflictf("role %q is still assigned to %d user(s)", role.Name, inUse).
			WithCode("roleStillAssigned", role.Name, inUse)
	}

	return s.roles.Delete(ctx, id)
}

// validateRole checks the name and rejects permissions the application does
// not enforce, so the UI cannot store a right that grants nothing.
func validateRole(name string, permissions []string) ([]string, error) {
	// Empty and longer than the column are both refused here: roles.name is
	// VARCHAR(64), which SQLite ignores and the servers do not.
	if strings.TrimSpace(name) == "" || model.TooLong(name, model.MaxRoleNameLength) {
		return nil, apperror.InvalidFields("name")
	}

	var unknown []string

	clean := make([]string, 0, len(permissions))

	for _, permission := range permissions {
		permission = strings.TrimSpace(permission)
		if permission == "" {
			continue
		}

		if !model.IsPermission(permission) {
			unknown = append(unknown, permission)

			continue
		}

		if !slices.Contains(clean, permission) {
			clean = append(clean, permission)
		}
	}

	if len(unknown) > 0 {
		return nil, apperror.Invalidf("unknown permission(s): %s", strings.Join(unknown, ", ")).
			WithCode("unknownPermissions", strings.Join(unknown, ", "))
	}

	// A role that grants nothing is a role somebody will assign and then wonder about.
	// Whoever holds it can sign in, see an interface with almost nothing on it, and
	// reach no screen that matters - which looks like a broken installation rather than
	// like a decision. If the intention is to take somebody's access away, that is what
	// removing the account is for.
	if len(clean) == 0 {
		return nil, apperror.Invalidf("a role has to grant at least one permission").
			WithCode("roleGrantsNothing")
	}

	return clean, nil
}

// sameRights reports whether two permission sets hold exactly the same names,
// whatever order they arrive in.
//
// Order and duplicates are the client's business, not a difference: a screen that
// sends its checkboxes back in a different sequence is not asking for a change, and
// refusing it as one would make the Save button fail for no reason anybody could see.
func sameRights(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	return grantsAtLeast(a, b) && grantsAtLeast(b, a)
}

// grantsAtLeast reports whether every permission in required is present in
// granted.
func grantsAtLeast(granted, required []string) bool {
	for _, permission := range required {
		if !slices.Contains(granted, permission) {
			return false
		}
	}

	return true
}
