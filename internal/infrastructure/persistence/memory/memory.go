// Package memory holds in-memory repository implementations. They back the
// unit tests for the application services, so those tests need no database.
package memory

import (
	"context"
	"strconv"
	"strings"
	"sync"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

// store is the shared bookkeeping behind the in-memory repositories: a
// monotonic id counter plus a mutex, since handlers run concurrently.
type store[T any] struct {
	mu     sync.RWMutex
	items  map[uint]*T
	nextID uint
	entity string
}

func newStore[T any](entity string) *store[T] {
	return &store[T]{items: make(map[uint]*T), nextID: 1, entity: entity}
}

func (s *store[T]) add(item *T, setID func(*T, uint)) *T {
	s.mu.Lock()
	defer s.mu.Unlock()

	stored := *item
	setID(&stored, s.nextID)
	s.items[s.nextID] = &stored
	s.nextID++

	// Hand back a copy so callers cannot mutate stored state by accident.
	out := stored

	return &out
}

func (s *store[T]) get(id uint) (*T, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	item, ok := s.items[id]
	if !ok {
		return nil, apperror.NotFound(s.entity, strconv.FormatUint(uint64(id), 10))
	}

	out := *item

	return &out, nil
}

func (s *store[T]) all() []*T {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Iterate ids in ascending order so results are stable across calls;
	// ranging a map directly would return a different order each time.
	out := make([]*T, 0, len(s.items))
	for id := uint(1); id < s.nextID; id++ {
		if item, ok := s.items[id]; ok {
			copied := *item
			out = append(out, &copied)
		}
	}

	return out
}

func (s *store[T]) update(id uint, item *T) (*T, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.items[id]; !ok {
		return nil, apperror.NotFound(s.entity, strconv.FormatUint(uint64(id), 10))
	}

	stored := *item
	s.items[id] = &stored
	out := stored

	return &out, nil
}

func (s *store[T]) delete(id uint) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.items[id]; !ok {
		return apperror.NotFound(s.entity, strconv.FormatUint(uint64(id), 10))
	}

	delete(s.items, id)

	return nil
}

// UserRepository is an in-memory repository.UserRepository.
type UserRepository struct {
	store *store[model.User]

	// roles resolves the role name on read. The SQL repository does this with
	// a join; without it here the two implementations would disagree and
	// tests against this one would give false confidence.
	roles *RoleRepository
}

// NewUserRepository creates an empty in-memory user repository.
func NewUserRepository() *UserRepository {
	return &UserRepository{store: newStore[model.User]("user")}
}

var _ repository.UserRepository = (*UserRepository)(nil)

// withRoleName fills in the display name for the user's role.
func (r *UserRepository) withRoleName(user *model.User) *model.User {
	if user == nil || r.roles == nil || user.RoleID == 0 {
		return user
	}

	if role, err := r.roles.store.get(user.RoleID); err == nil {
		user.RoleName = role.Name
	}

	return user
}

func (r *UserRepository) Save(_ context.Context, user *model.User) (*model.User, error) {
	r.store.mu.RLock()
	for _, existing := range r.store.items {
		if existing.Email == user.Email {
			r.store.mu.RUnlock()

			return nil, apperror.Conflictf("a user with email %q already exists", user.Email).
				WithCode("emailTaken", user.Email)
		}
	}
	r.store.mu.RUnlock()

	return r.withRoleName(r.store.add(user, func(u *model.User, id uint) { u.ID = id })), nil
}

func (r *UserRepository) GetByID(_ context.Context, id uint) (*model.User, error) {
	user, err := r.store.get(id)
	if err != nil {
		return nil, err
	}

	return r.withRoleName(user), nil
}

func (r *UserRepository) GetByEmail(_ context.Context, email string) (*model.User, error) {
	r.store.mu.RLock()
	defer r.store.mu.RUnlock()

	for _, user := range r.store.items {
		if strings.EqualFold(user.Email, email) {
			found := *user

			return r.withRoleName(&found), nil
		}
	}

	return nil, apperror.NotFound("user", email)
}

func (r *UserRepository) GetByExternalID(_ context.Context, externalID string) (*model.User, error) {
	if externalID == "" {
		return nil, apperror.NotFound("user", "")
	}

	r.store.mu.RLock()
	defer r.store.mu.RUnlock()

	for _, user := range r.store.items {
		if user.ExternalID == externalID {
			found := *user

			return r.withRoleName(&found), nil
		}
	}

	return nil, apperror.NotFound("user", externalID)
}

func (r *UserRepository) GetAll(_ context.Context) ([]*model.User, error) {
	users := r.store.all()
	for _, user := range users {
		r.withRoleName(user)
	}

	return users, nil
}

func (r *UserRepository) Update(_ context.Context, user *model.User) (*model.User, error) {
	updated, err := r.store.update(user.ID, user)
	if err != nil {
		return nil, err
	}

	return r.withRoleName(updated), nil
}

func (r *UserRepository) Delete(_ context.Context, id uint) error {
	return r.store.delete(id)
}

// RoleRepository is an in-memory repository.RoleRepository.
type RoleRepository struct {
	store *store[model.Role]
	users *UserRepository
}

// NewRoleRepository creates an in-memory role repository seeded with the
// default roles, matching what the migration does for a real database.
func NewRoleRepository(users *UserRepository) *RoleRepository {
	r := &RoleRepository{store: newStore[model.Role]("role"), users: users}

	// Register both ways so a user read can resolve its role name, matching
	// what the SQL repository gets from a join.
	if users != nil {
		users.roles = r
	}

	for _, role := range model.DefaultRoles() {
		seeded := role
		r.store.add(&seeded, func(x *model.Role, id uint) { x.ID = id })
	}

	return r
}

var _ repository.RoleRepository = (*RoleRepository)(nil)

func (r *RoleRepository) Save(_ context.Context, role *model.Role) (*model.Role, error) {
	return r.store.add(role, func(x *model.Role, id uint) { x.ID = id }), nil
}

func (r *RoleRepository) GetByID(_ context.Context, id uint) (*model.Role, error) {
	return r.store.get(id)
}

func (r *RoleRepository) GetByName(_ context.Context, name string) (*model.Role, error) {
	for _, role := range r.store.all() {
		if strings.EqualFold(role.Name, name) {
			return role, nil
		}
	}

	return nil, apperror.NotFound("role", name)
}

func (r *RoleRepository) GetAll(_ context.Context) ([]*model.Role, error) {
	return r.store.all(), nil
}

func (r *RoleRepository) Update(_ context.Context, role *model.Role) (*model.Role, error) {
	return r.store.update(role.ID, role)
}

func (r *RoleRepository) Delete(_ context.Context, id uint) error {
	return r.store.delete(id)
}

func (r *RoleRepository) CountUsers(ctx context.Context, roleID uint) (int, error) {
	if r.users == nil {
		return 0, nil
	}

	all, err := r.users.GetAll(ctx)
	if err != nil {
		return 0, err
	}

	var count int

	for _, user := range all {
		if user.RoleID == roleID {
			count++
		}
	}

	return count, nil
}

// ProjectRepository is an in-memory repository.ProjectRepository.
type ProjectRepository struct{ store *store[model.Project] }

// NewProjectRepository creates an empty in-memory project repository.
func NewProjectRepository() *ProjectRepository {
	return &ProjectRepository{store: newStore[model.Project]("project")}
}

var _ repository.ProjectRepository = (*ProjectRepository)(nil)

func (r *ProjectRepository) Save(_ context.Context, project *model.Project) (*model.Project, error) {
	return r.store.add(project, func(p *model.Project, id uint) { p.ID = id }), nil
}

func (r *ProjectRepository) GetByID(_ context.Context, id uint) (*model.Project, error) {
	return r.store.get(id)
}

func (r *ProjectRepository) GetAll(_ context.Context) ([]*model.Project, error) {
	return r.store.all(), nil
}

func (r *ProjectRepository) Update(_ context.Context, project *model.Project) (*model.Project, error) {
	return r.store.update(project.ID, project)
}

func (r *ProjectRepository) Delete(_ context.Context, id uint) error {
	return r.store.delete(id)
}

// TimesheetRepository is an in-memory repository.TimesheetRepository.
type TimesheetRepository struct{ store *store[model.Timesheet] }

// NewTimesheetRepository creates an empty in-memory timesheet repository.
func NewTimesheetRepository() *TimesheetRepository {
	return &TimesheetRepository{store: newStore[model.Timesheet]("timesheet")}
}

var _ repository.TimesheetRepository = (*TimesheetRepository)(nil)

func (r *TimesheetRepository) Save(_ context.Context, timesheet *model.Timesheet) (*model.Timesheet, error) {
	return r.store.add(timesheet, func(t *model.Timesheet, id uint) { t.ID = id }), nil
}

func (r *TimesheetRepository) GetByID(_ context.Context, id uint) (*model.Timesheet, error) {
	return r.store.get(id)
}

func (r *TimesheetRepository) GetAll(_ context.Context) ([]*model.Timesheet, error) {
	return r.store.all(), nil
}

func (r *TimesheetRepository) GetByFilter(
	_ context.Context,
	filter repository.TimesheetFilter,
) ([]*model.Timesheet, error) {
	out := make([]*model.Timesheet, 0)

	for _, ts := range r.store.all() {
		if !matchesFilter(ts, filter) {
			continue
		}

		out = append(out, ts)
	}

	return out, nil
}

func matchesFilter(ts *model.Timesheet, filter repository.TimesheetFilter) bool {
	switch {
	case filter.UserID != 0 && ts.UserID != filter.UserID:
		return false
	case filter.ProjectID != 0 && (!ts.HasProject() || *ts.ProjectID != filter.ProjectID):
		return false
	case filter.WithoutProject && ts.HasProject():
		return false
	case filter.Status != "" && ts.Status != filter.Status:
		return false
	case filter.StartDate != nil && ts.Date.Before(*filter.StartDate):
		return false
	case filter.EndDate != nil && ts.Date.After(*filter.EndDate):
		return false
	default:
		return true
	}
}

func (r *TimesheetRepository) Update(_ context.Context, timesheet *model.Timesheet) (*model.Timesheet, error) {
	return r.store.update(timesheet.ID, timesheet)
}

func (r *TimesheetRepository) Delete(_ context.Context, id uint) error {
	return r.store.delete(id)
}
