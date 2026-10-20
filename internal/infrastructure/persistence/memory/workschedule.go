package memory

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
)

// WorkScheduleRepository is an in-memory repository.WorkScheduleRepository.
type WorkScheduleRepository struct {
	mu        sync.RWMutex
	schedules []model.WorkSchedule
}

// NewWorkScheduleRepository creates an empty one.
func NewWorkScheduleRepository() *WorkScheduleRepository {
	return &WorkScheduleRepository{}
}

var _ repository.WorkScheduleRepository = (*WorkScheduleRepository)(nil)

func (r *WorkScheduleRepository) ForUser(_ context.Context, userID uint) ([]model.WorkSchedule, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var own []model.WorkSchedule

	for _, schedule := range r.schedules {
		if schedule.UserID == userID {
			own = append(own, schedule)
		}
	}

	// Earliest first, as the SQL repository's ORDER BY returns them.
	slices.SortFunc(own, func(a, b model.WorkSchedule) int {
		return a.ValidFrom.Compare(b.ValidFrom)
	})

	return own, nil
}

func (r *WorkScheduleRepository) Save(_ context.Context, schedule model.WorkSchedule) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i, existing := range r.schedules {
		if existing.UserID == schedule.UserID && existing.ValidFrom.Equal(schedule.ValidFrom) {
			r.schedules[i] = schedule

			return nil
		}
	}

	r.schedules = append(r.schedules, schedule)

	return nil
}

func (r *WorkScheduleRepository) Delete(_ context.Context, userID uint, validFrom time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i, existing := range r.schedules {
		if existing.UserID == userID && existing.ValidFrom.Equal(validFrom) {
			r.schedules = slices.Delete(r.schedules, i, i+1)

			return true, nil
		}
	}

	return false, nil
}
