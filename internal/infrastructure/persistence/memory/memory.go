// Package memory holds in-memory repository implementations. They back the
// unit tests for the application services, so those tests need no database.
package memory

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

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

// SetPreference writes one field, which in memory means changing it on the stored
// record rather than replacing the record.
//
// The distinction matters here as much as in SQL: the tests that drive the services
// through this store are the ones that would otherwise pass while the real
// repository loses a concurrent change.
func (r *UserRepository) SetPreference(
	_ context.Context,
	id uint,
	field repository.Preference,
	value string,
) error {
	current, err := r.store.get(id)
	if err != nil {
		return err
	}

	switch field {
	case repository.PreferenceTourSeen:
		current.TourSeen = value == "true"
	case repository.PreferenceLanguage:
		current.Language = value
	case repository.PreferenceTimezone:
		current.Timezone = value
	default:
		return fmt.Errorf("unknown user preference %d", field)
	}

	_, err = r.store.update(id, current)

	return err
}

// SetTOTP writes the second factor's two fields, leaving the rest as they are.
func (r *UserRepository) SetTOTP(_ context.Context, id uint, secret string, enabled bool) error {
	current, err := r.store.get(id)
	if err != nil {
		return err
	}

	current.TOTPSecret, current.TOTPEnabled = secret, enabled

	_, err = r.store.update(id, current)

	return err
}

func (r *UserRepository) Delete(_ context.Context, id uint) error {
	return r.store.delete(id)
}

// SaveMany writes several entries.
//
// Not atomic, and it cannot be: this store has no transaction to roll back to.
// The guarantee belongs to the SQL repository, and the integration tests are where
// it is checked - a unit test against this would be checking nothing.
func (r *TimesheetRepository) SaveMany(ctx context.Context, entries []*model.Timesheet) error {
	for _, entry := range entries {
		if _, err := r.Save(ctx, entry); err != nil {
			return err
		}
	}

	return nil
}

// TimerRepository is an in-memory repository.TimerRepository.
//
// Keyed by user rather than by an id of its own, because a person has at most one
// clock running - which is what the real table says too, with user_id as its
// primary key.
type TimerRepository struct {
	mu     sync.RWMutex
	timers map[uint]model.RunningTimer
}

// NewTimerRepository creates an empty one.
func NewTimerRepository() *TimerRepository {
	return &TimerRepository{timers: map[uint]model.RunningTimer{}}
}

var _ repository.TimerRepository = (*TimerRepository)(nil)

func (r *TimerRepository) Get(_ context.Context, userID uint) (*model.RunningTimer, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	timer, running := r.timers[userID]
	if !running {
		// Nothing running is a normal state, not an error.
		return nil, nil
	}

	found := timer

	return &found, nil
}

func (r *TimerRepository) Start(_ context.Context, timer *model.RunningTimer) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.timers[timer.UserID] = *timer

	return nil
}

func (r *TimerRepository) Clear(_ context.Context, userID uint) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.timers, userID)

	return nil
}

func (r *TimerRepository) CountByProject(_ context.Context, projectID uint) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var count int

	for _, timer := range r.timers {
		if timer.ProjectID != nil && *timer.ProjectID == projectID {
			count++
		}
	}

	return count, nil
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

// Save stamps the recording moment the same way the SQL repository does.
//
// Not a detail the in-memory store may skip: a service test that checks an entry
// knows when it was booked would pass here and fail against a database, or the
// reverse, and either way the suite would be proving something about a store
// nobody deploys.
func (r *TimesheetRepository) Save(_ context.Context, timesheet *model.Timesheet) (*model.Timesheet, error) {
	// Truncated the way the SQL repository truncates, and for its reason: a store
	// whose stamps are finer than any database's would let a test pass here that
	// cannot pass against PostgreSQL.
	now := time.Now().UTC().Truncate(time.Second)

	return r.store.add(timesheet, func(t *model.Timesheet, id uint) {
		t.ID = id
		t.CreatedAt, t.UpdatedAt = now, now
	}), nil
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
	// The same narrowing the SQL repository applies, for the same reason: a range
	// is a range of days. Two repositories that disagree about which entries a
	// month contains would be worse than either answer alone.
	filter = filter.OverWholeDays()

	out := make([]*model.Timesheet, 0)

	for _, ts := range r.store.all() {
		if !matchesFilter(ts, filter) {
			continue
		}

		out = append(out, ts)
	}

	// The same order the SQL repository gives, because paging is defined in terms
	// of it: newest day first, and an entry booked later on the same day ahead of
	// one booked earlier. The in-memory store hands rows back in insertion order,
	// which agrees with neither.
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Date.Equal(out[j].Date) {
			return out[i].Date.After(out[j].Date)
		}

		return out[i].ID > out[j].ID
	})

	return page(out, filter.Limit, filter.Offset), nil
}

// CountByFilter counts the matching entries, ignoring Limit and Offset.
func (r *TimesheetRepository) CountByFilter(
	_ context.Context,
	filter repository.TimesheetFilter,
) (uint, error) {
	filter = filter.OverWholeDays()

	var total uint

	for _, ts := range r.store.all() {
		if matchesFilter(ts, filter) {
			total++
		}
	}

	return total, nil
}

// page applies a limit and an offset the way a SQL LIMIT/OFFSET would, including
// answering with nothing for an offset past the end rather than with the last
// page, which is what a hand-rolled slice usually gets wrong.
func page[T any](all []T, limit, offset uint) []T {
	if limit == 0 {
		return all
	}

	if offset >= uint(len(all)) {
		return all[:0]
	}

	end := offset + limit
	if end > uint(len(all)) {
		end = uint(len(all))
	}

	return all[offset:end]
}

func matchesFilter(ts *model.Timesheet, filter repository.TimesheetFilter) bool {
	switch {
	case filter.UserID != 0 && ts.UserID != filter.UserID:
		return false
	case filter.ProjectID != 0 && (!ts.HasProject() || *ts.ProjectID != filter.ProjectID):
		return false
	case filter.WithoutProject && ts.HasProject():
		return false
	case filter.StartDate != nil && ts.Date.Before(*filter.StartDate):
		return false
	case filter.EndDate != nil && ts.Date.After(*filter.EndDate):
		return false
	default:
		return true
	}
}

// Update moves the correction moment and keeps the recording one.
//
// The caller hands over a whole entry, and the one it was given may have travelled
// through a DTO that never carried created_at - so the stored value is read back
// rather than trusted, which is what the SQL repository gets for free by leaving
// the column out of its UPDATE.
func (r *TimesheetRepository) Update(_ context.Context, timesheet *model.Timesheet) (*model.Timesheet, error) {
	corrected := *timesheet
	corrected.UpdatedAt = time.Now().UTC().Truncate(time.Second)

	if existing, err := r.store.get(timesheet.ID); err == nil {
		corrected.CreatedAt = existing.CreatedAt
	}

	return r.store.update(timesheet.ID, &corrected)
}

func (r *TimesheetRepository) Delete(_ context.Context, id uint) error {
	return r.store.delete(id)
}

// SessionRepository keeps signed-in sessions in memory.
//
// Written so the unit tests can reach the real sign-in path. Without it,
// AuthService.Authenticate was the only way for a test to turn an address and a
// password into a principal - a second implementation of the password check that
// the application itself never called, so the tests were proving something about
// code nobody ran. Whether a wrong password is refused is a question about
// SessionService, and now they can ask it there.
//
// Keyed by the stored hash rather than by an id, because that is the only way a
// session is ever looked up: the browser presents a token and the hash of it is
// the whole of what identifies the row.
type SessionRepository struct {
	mu       sync.RWMutex
	sessions map[string]*model.Session
}

// NewSessionRepository creates an empty session store.
func NewSessionRepository() *SessionRepository {
	return &SessionRepository{sessions: make(map[string]*model.Session)}
}

var _ repository.SessionRepository = (*SessionRepository)(nil)

func (r *SessionRepository) Save(_ context.Context, session *model.Session) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Copied in, so a caller that reuses its struct cannot rewrite what is stored.
	stored := *session
	r.sessions[session.TokenHash] = &stored

	return nil
}

func (r *SessionRepository) Get(_ context.Context, tokenHash string) (*model.Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	session, ok := r.sessions[tokenHash]
	if !ok {
		return nil, apperror.NotFound("session", tokenHash)
	}

	out := *session

	return &out, nil
}

// Touch records that the session was used, for the idle timeout to measure
// against.
func (r *SessionRepository) Touch(_ context.Context, tokenHash string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	session, ok := r.sessions[tokenHash]
	if !ok {
		return apperror.NotFound("session", tokenHash)
	}

	session.LastSeenAt = at

	return nil
}

func (r *SessionRepository) Delete(_ context.Context, tokenHash string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.sessions, tokenHash)

	return nil
}

func (r *SessionRepository) DeleteForUser(_ context.Context, userID uint) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for hash, session := range r.sessions {
		if session.UserID == userID {
			delete(r.sessions, hash)
		}
	}

	return nil
}

func (r *SessionRepository) DeleteForUserExcept(
	_ context.Context,
	userID uint,
	keepTokenHash string,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for hash, session := range r.sessions {
		if session.UserID == userID && hash != keepTokenHash {
			delete(r.sessions, hash)
		}
	}

	return nil
}

func (r *SessionRepository) DeleteExpired(_ context.Context) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var removed int64

	now := time.Now()

	for hash, session := range r.sessions {
		if session.Expired(now) {
			delete(r.sessions, hash)

			removed++
		}
	}

	return removed, nil
}
