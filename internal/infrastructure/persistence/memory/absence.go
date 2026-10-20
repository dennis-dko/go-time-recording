package memory

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
)

// AbsenceRepository is an in-memory repository.AbsenceRepository.
type AbsenceRepository struct {
	mu       sync.RWMutex
	absences []model.Absence
}

// NewAbsenceRepository creates an empty one.
func NewAbsenceRepository() *AbsenceRepository {
	return &AbsenceRepository{}
}

var _ repository.AbsenceRepository = (*AbsenceRepository)(nil)

func (r *AbsenceRepository) Between(_ context.Context, userID uint, from, to time.Time) ([]model.Absence, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	from, to = model.CalendarDay(from), model.CalendarDay(to)

	var found []model.Absence

	for _, absence := range r.absences {
		if absence.UserID == userID && !absence.Day.Before(from) && !absence.Day.After(to) {
			found = append(found, absence)
		}
	}

	// Earliest first, as the SQL repository's ORDER BY returns them.
	slices.SortFunc(found, func(a, b model.Absence) int { return a.Day.Compare(b.Day) })

	return found, nil
}

func (r *AbsenceRepository) Save(_ context.Context, absences []model.Absence) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, absence := range absences {
		absence.Day = model.CalendarDay(absence.Day)

		at := slices.IndexFunc(r.absences, func(kept model.Absence) bool {
			return kept.UserID == absence.UserID && kept.Day.Equal(absence.Day)
		})

		if at >= 0 {
			r.absences[at] = absence
		} else {
			r.absences = append(r.absences, absence)
		}
	}

	return nil
}

func (r *AbsenceRepository) Delete(_ context.Context, userID uint, day time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	day = model.CalendarDay(day)

	for i, kept := range r.absences {
		if kept.UserID == userID && kept.Day.Equal(day) {
			r.absences = slices.Delete(r.absences, i, i+1)

			return true, nil
		}
	}

	return false, nil
}
