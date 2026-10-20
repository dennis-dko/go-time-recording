package memory

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
)

// WorkDayRepository is an in-memory repository.WorkDayRepository.
type WorkDayRepository struct {
	mu   sync.RWMutex
	days []model.WorkDay
}

// NewWorkDayRepository creates an empty one.
func NewWorkDayRepository() *WorkDayRepository {
	return &WorkDayRepository{}
}

var _ repository.WorkDayRepository = (*WorkDayRepository)(nil)

func (r *WorkDayRepository) Between(_ context.Context, userID uint, from, to time.Time) ([]model.WorkDay, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	first, last := model.CalendarDay(from), model.CalendarDay(to)

	var own []model.WorkDay

	for _, day := range r.days {
		if day.UserID == userID && !day.Day.Before(first) && !day.Day.After(last) {
			own = append(own, day)
		}
	}

	// Earliest first, as the SQL repository's ORDER BY returns them.
	slices.SortFunc(own, func(a, b model.WorkDay) int { return a.Day.Compare(b.Day) })

	return own, nil
}

func (r *WorkDayRepository) Save(_ context.Context, day model.WorkDay) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	day.Day = model.CalendarDay(day.Day)

	for i, existing := range r.days {
		if existing.UserID == day.UserID && existing.Day.Equal(day.Day) {
			r.days[i] = day

			return nil
		}
	}

	r.days = append(r.days, day)

	return nil
}

func (r *WorkDayRepository) Delete(_ context.Context, userID uint, day time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	day = model.CalendarDay(day)

	for i, existing := range r.days {
		if existing.UserID == userID && existing.Day.Equal(day) {
			r.days = slices.Delete(r.days, i, i+1)

			return true, nil
		}
	}

	return false, nil
}
