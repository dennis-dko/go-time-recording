// Package memory holds in-memory repository implementations. They back the
// unit tests for the application services, so those tests need no database.
package memory

import (
	"context"
	"strconv"
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
type UserRepository struct{ store *store[model.User] }

// NewUserRepository creates an empty in-memory user repository.
func NewUserRepository() *UserRepository {
	return &UserRepository{store: newStore[model.User]("user")}
}

var _ repository.UserRepository = (*UserRepository)(nil)

func (r *UserRepository) Save(_ context.Context, user *model.User) (*model.User, error) {
	r.store.mu.RLock()
	for _, existing := range r.store.items {
		if existing.Email == user.Email {
			r.store.mu.RUnlock()

			return nil, apperror.Conflictf("a user with email %q already exists", user.Email)
		}
	}
	r.store.mu.RUnlock()

	return r.store.add(user, func(u *model.User, id uint) { u.ID = id }), nil
}

func (r *UserRepository) GetByID(_ context.Context, id uint) (*model.User, error) {
	return r.store.get(id)
}

func (r *UserRepository) GetAll(_ context.Context) ([]*model.User, error) {
	return r.store.all(), nil
}

func (r *UserRepository) Update(_ context.Context, user *model.User) (*model.User, error) {
	return r.store.update(user.ID, user)
}

func (r *UserRepository) Delete(_ context.Context, id uint) error {
	return r.store.delete(id)
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
	case filter.ProjectID != 0 && ts.ProjectID != filter.ProjectID:
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
