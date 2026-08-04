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
	if err := validateWorkingTimes(cmd.DailyTargetHours, cmd.MaxDailyHours); err != nil {
		return nil, err
	}

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
		return nil, apperror.Invalidf("%v", err)
	}

	createdUser, err := s.userRepository.Save(ctx, &model.User{
		Name:               cmd.Name,
		Email:              normalizeEmail(cmd.Email),
		RoleID:             role.ID,
		PasswordHash:       hash,
		MustChangePassword: mustChange,
		DailyTargetHours:   cmd.DailyTargetHours,
		MaxDailyHours:      cmd.MaxDailyHours,
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
				role.Name, model.PermRoleWrite)
		}

		existingUser.RoleID = role.ID
	}

	if cmd.DailyTargetHours != nil {
		existingUser.DailyTargetHours = *cmd.DailyTargetHours
	}

	if cmd.MaxDailyHours != nil {
		existingUser.MaxDailyHours = *cmd.MaxDailyHours
	}

	if err := validateUser(existingUser.Name, existingUser.Email); err != nil {
		return nil, err
	}

	if err := validateWorkingTimes(existingUser.DailyTargetHours, existingUser.MaxDailyHours); err != nil {
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
		return apperror.Conflictf("the built-in administrator cannot be deleted")
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
			user.Email, len(entries))
	}

	return s.purger.PurgeUser(ctx, cmd.ID)
}

// resolveRole accepts a role name, defaulting to the least privileged role
// when none is given.
func (s *UserApplicationService) resolveRole(ctx context.Context, name string) (*model.Role, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = model.RoleEmployee
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
			target, maxDaily)
	}

	if len(invalid) > 0 {
		return apperror.InvalidFields(invalid...)
	}

	return nil
}
