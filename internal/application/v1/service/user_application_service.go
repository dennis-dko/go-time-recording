package service

import (
	"context"
	"strings"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/common"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/query"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
	"github.com/dennis-dko/go-time-recording/internal/pkg/security"
)

// UserService service interface
type UserService interface {
	CreateUser(ctx context.Context, cmd command.CreateUserCommand) (*command.CreateUserCommandResult, error)
	GetUser(ctx context.Context, q query.GetUserQuery) (*query.GetUserQueryResult, error)
	ListUsers(ctx context.Context, q query.ListUsersQuery) (*query.ListUsersQueryResult, error)
	UpdateUser(ctx context.Context, cmd command.UpdateUserCommand) (*command.UpdateUserCommandResult, error)
	DeleteUser(ctx context.Context, cmd command.DeleteUserCommand) error
	UpdateWorkingTimes(ctx context.Context, cmd command.UpdateWorkingTimesCommand) (*common.UserResult, error)
}

// UserApplicationService application service for users
type UserApplicationService struct {
	userRepository repository.UserRepository
	roleRepository repository.RoleRepository

	// timesheetRepository answers how much recorded time an account has, which
	// is what a deletion has to warn about.
	timesheetRepository repository.TimesheetRepository

	// purger removes an account together with everything referencing it.
	//
	// Deleting only the account row is not an option: the schema declares
	// foreign keys, so a database that enforces them refuses it outright, and
	// SQLite - where they are off unless asked for - accepts it and leaves the
	// hours behind pointing at nobody. Same request, two different wrong
	// answers, depending on a choice made in the installer.
	purger UserPurger
}

// NewUserApplicationService creates new instance
func NewUserApplicationService(
	userRepo repository.UserRepository,
	roleRepo repository.RoleRepository,
	timesheetRepo repository.TimesheetRepository,
	purger UserPurger,
) *UserApplicationService {
	return &UserApplicationService{
		userRepository:      userRepo,
		roleRepository:      roleRepo,
		timesheetRepository: timesheetRepo,
		purger:              purger,
	}
}

var _ UserService = (*UserApplicationService)(nil)

// CreateUser processes the command to create a user
func (s *UserApplicationService) CreateUser(
	ctx context.Context,
	cmd command.CreateUserCommand,
) (*command.CreateUserCommandResult, error) {
	if err := validateUser(cmd.Name, cmd.Email); err != nil {
		return nil, err
	}

	// The same bounds the working-times screen enforces. Without this an account
	// could be created with a negative daily target that no later edit was
	// required to fix, and every overtime balance computed from it would be
	// wrong in a direction nobody would think to question.

	role, err := s.resolveRole(ctx, cmd.Role)
	if err != nil {
		return nil, err
	}

	// A user with no password cannot sign in, so a new account either gets the
	// password it was given or one that must be replaced on first use.
	password := cmd.Password
	mustChange := false

	if password == "" {
		password = SystemUserPassword
		mustChange = true
	}

	hash, err := security.HashPassword(password)
	if err != nil {
		return nil, passwordError(err)
	}

	createdUser, err := s.userRepository.Save(ctx, &model.User{
		Name:               cmd.Name,
		Email:              normalizeEmail(cmd.Email),
		RoleID:             role.ID,
		PasswordHash:       hash,
		MustChangePassword: mustChange,
		// Zero, which the reader resolves to the instance default. A new account
		// starts on what the installation is configured for; its owner changes it
		// afterwards, and nobody else can.
		DailyTargetHours: 0,
		MaxDailyHours:    0,
	})
	if err != nil {
		return nil, err
	}

	return &command.CreateUserCommandResult{
		Result: common.NewUserResultFromModel(createdUser)[0],
	}, nil
}

// GetUser processes the query to get a user
func (s *UserApplicationService) GetUser(
	ctx context.Context,
	q query.GetUserQuery,
) (*query.GetUserQueryResult, error) {
	if q.ID == 0 {
		return nil, apperror.InvalidFields("id")
	}

	user, err := s.userRepository.GetByID(ctx, q.ID)
	if err != nil {
		return nil, err
	}

	return &query.GetUserQueryResult{
		Result: common.NewUserResultFromModel(user)[0],
	}, nil
}

// ListUsers processes the query to get all users, applying optional paging.
func (s *UserApplicationService) ListUsers(
	ctx context.Context,
	q query.ListUsersQuery,
) (*query.ListUsersQueryResult, error) {
	allUsers, err := s.userRepository.GetAll(ctx)
	if err != nil {
		return nil, err
	}

	// TotalCount reports the unpaged total so a client can size its pager.
	total := uint(len(allUsers))

	return &query.ListUsersQueryResult{
		Result:     common.NewUserResultFromModel(paginate(allUsers, q.Page, q.Limit)...),
		TotalCount: total,
	}, nil
}

// UpdateUser processes the command to update a user
func (s *UserApplicationService) UpdateUser(
	ctx context.Context,
	cmd command.UpdateUserCommand,
) (*command.UpdateUserCommandResult, error) {
	if cmd.ID == 0 {
		return nil, apperror.InvalidFields("id")
	}

	existingUser, err := s.userRepository.GetByID(ctx, cmd.ID)
	if err != nil {
		return nil, err
	}

	// An account the directory owns is edited in the directory, with one
	// exception.
	//
	// The name and the address are copied from the entry on every
	// synchronisation, so changing them here lasts until the next run and then
	// silently reverts - which is worse than being refused, because it looks like
	// it worked. The role is the exception because the directory has no opinion
	// about it: it is decided here, for a person the directory only says exists.
	if existingUser.IsExternal {
		if cmd.Name != nil && *cmd.Name != existingUser.Name {
			return nil, apperror.Conflictf(
				"%q comes from the directory; the name is kept in step with the entry "+
					"there and changing it here would last until the next "+
					"synchronisation", existingUser.Email).
				WithCode("directoryAccountReadOnly", existingUser.Email)
		}

		if cmd.Email != nil && normalizeEmail(*cmd.Email) != existingUser.Email {
			return nil, apperror.Conflictf(
				"%q comes from the directory; the address is what the synchronisation "+
					"matches on and is not changed here", existingUser.Email).
				WithCode("directoryAccountReadOnly", existingUser.Email)
		}
	}

	if cmd.Name != nil {
		existingUser.Name = *cmd.Name
	}

	if cmd.Email != nil {
		existingUser.Email = normalizeEmail(*cmd.Email)
	}

	if cmd.Role != nil {
		role, roleErr := s.resolveRole(ctx, *cmd.Role)
		if roleErr != nil {
			return nil, roleErr
		}

		// Same protection as the domain service: the built-in administrator
		// must keep a role that can still administer.
		if existingUser.IsSystem && !role.Has(model.PermRoleWrite) {
			return nil, apperror.Conflictf(
				"the built-in administrator cannot be moved to role %q, which lacks %q",
				role.Name, model.PermRoleWrite).
				WithCode("adminRoleMustAdminister", role.Name, model.PermRoleWrite)
		}

		existingUser.RoleID = role.ID
	}

	// The daily target and the ceiling are deliberately not here. They are time
	// figures and belong to whoever they are about, who sets them through
	// UpdateWorkingTimes - the one path that asks whose account it is.
	//
	// They used to be writable here too, on users:write alone, which made the right
	// that was supposed to guard them one lock on a door with three ways in.
	if err := validateUser(existingUser.Name, existingUser.Email); err != nil {
		return nil, err
	}

	updatedUser, err := s.userRepository.Update(ctx, existingUser)
	if err != nil {
		return nil, err
	}

	return &command.UpdateUserCommandResult{
		Result: common.NewUserResultFromModel(updatedUser)[0],
	}, nil
}

// UpdateWorkingTimes sets the daily target and cap for a user. It is separate
// from UpdateUser so a user may adjust their own working times without also
// holding the right to edit accounts.
func (s *UserApplicationService) UpdateWorkingTimes(
	ctx context.Context,
	cmd command.UpdateWorkingTimesCommand,
) (*common.UserResult, error) {
	if cmd.ID == 0 {
		return nil, apperror.InvalidFields("id")
	}

	user, err := s.userRepository.GetByID(ctx, cmd.ID)
	if err != nil {
		return nil, err
	}

	if cmd.DailyTargetHours != nil {
		user.DailyTargetHours = *cmd.DailyTargetHours
	}

	if cmd.MaxDailyHours != nil {
		user.MaxDailyHours = *cmd.MaxDailyHours
	}

	if err := validateWorkingTimes(user.DailyTargetHours, user.MaxDailyHours); err != nil {
		return nil, err
	}

	updated, err := s.userRepository.Update(ctx, user)
	if err != nil {
		return nil, err
	}

	return common.NewUserResultFromModel(updated)[0], nil
}

// DeleteUser processes the command to delete a user
func (s *UserApplicationService) DeleteUser(ctx context.Context, cmd command.DeleteUserCommand) error {
	if cmd.ID == 0 {
		return apperror.InvalidFields("id")
	}

	user, err := s.userRepository.GetByID(ctx, cmd.ID)
	if err != nil {
		return err
	}

	// Deleting the built-in administrator would leave an installation with no
	// guaranteed way back in.
	if user.IsSystem {
		return apperror.Conflictf("the built-in administrator cannot be deleted").
			WithCode("adminUndeletable")
	}

	// An account the directory owns is removed by removing the entry there.
	//
	// Deleting it here removes a person and, with --purge, everything they
	// recorded - and the next synchronisation creates the account again from the
	// entry that is still in the directory. So the hours are gone, the account is
	// back, and nothing about the directory has changed. Whoever meant to remove
	// this person did not mean that.
	if user.IsExternal {
		return apperror.Conflictf(
			"%q comes from the directory; remove the entry there and the next "+
				"synchronisation removes this account", user.Email).
			WithCode("directoryAccountUndeletable", user.Email)
	}

	// How much of the person's work is about to go with the account.
	entries, err := s.timesheetRepository.GetByFilter(ctx,
		repository.TimesheetFilter{UserID: cmd.ID})
	if err != nil {
		return err
	}

	if len(entries) > 0 && !cmd.Purge {
		return apperror.Conflictf(
			"%q has %d recorded time entries, which would be deleted with the account "+
				"and cannot be recovered; confirm to proceed",
			user.Email, len(entries)).
			WithCode("deletionNeedsConfirming", user.Email, len(entries))
	}

	return s.purger.PurgeUser(ctx, cmd.ID)
}

// resolveRole accepts a role name, defaulting to the least privileged role
// when none is given.
func (s *UserApplicationService) resolveRole(ctx context.Context, name string) (*model.Role, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = model.RoleUser
	}

	role, err := s.roleRepository.GetByName(ctx, name)
	if err != nil {
		return nil, apperror.InvalidFields("role")
	}

	return role, nil
}

func validateUser(name, email string) error {
	var invalid []string

	// Empty and too long are both "not a name", and both have to be caught
	// here: the column enforces the second only on PostgreSQL and MySQL, so on
	// SQLite it is stored and everywhere else it is a driver error the caller
	// reads as a broken application.
	if strings.TrimSpace(name) == "" || model.TooLong(name, model.MaxNameLength) {
		invalid = append(invalid, "name")
	}

	if !validEmail(email) || model.TooLong(email, model.MaxEmailLength) {
		invalid = append(invalid, "email")
	}

	if len(invalid) > 0 {
		return apperror.InvalidFields(invalid...)
	}

	return nil
}

// validateWorkingTimes keeps the pair coherent: 0 means "use the default", and
// a target above the cap could never be reached.
func validateWorkingTimes(target, maxDaily float64) error {
	var invalid []string

	if target < 0 || target > 24 {
		invalid = append(invalid, "dailyTargetHours")
	}

	if maxDaily < 0 || maxDaily > 24 {
		invalid = append(invalid, "maxDailyHours")
	}

	if len(invalid) == 0 && target > 0 && maxDaily > 0 && target > maxDaily {
		return apperror.Invalidf("the daily target (%.2fh) cannot exceed the daily maximum (%.2fh)",
			target, maxDaily).
			WithCode("targetOverMaximum", target, maxDaily)
	}

	if len(invalid) > 0 {
		return apperror.InvalidFields(invalid...)
	}

	return nil
}
