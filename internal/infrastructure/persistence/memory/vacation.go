package memory

import (
	"cmp"
	"context"
	"slices"
	"sync"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
)

// VacationRepository is an in-memory repository.VacationRepository.
type VacationRepository struct {
	mu           sync.RWMutex
	entitlements []model.VacationEntitlement
}

// NewVacationRepository creates an empty one.
func NewVacationRepository() *VacationRepository {
	return &VacationRepository{}
}

var _ repository.VacationRepository = (*VacationRepository)(nil)

func (r *VacationRepository) ForUser(_ context.Context, userID uint) ([]model.VacationEntitlement, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var own []model.VacationEntitlement

	for _, entitlement := range r.entitlements {
		if entitlement.UserID == userID {
			own = append(own, entitlement)
		}
	}

	// Earliest first, as the SQL repository's ORDER BY returns them.
	slices.SortFunc(own, func(a, b model.VacationEntitlement) int { return cmp.Compare(a.FromYear, b.FromYear) })

	return own, nil
}

func (r *VacationRepository) Save(_ context.Context, entitlement model.VacationEntitlement) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i, existing := range r.entitlements {
		if existing.UserID == entitlement.UserID && existing.FromYear == entitlement.FromYear {
			r.entitlements[i] = entitlement

			return nil
		}
	}

	r.entitlements = append(r.entitlements, entitlement)

	return nil
}

func (r *VacationRepository) Delete(_ context.Context, userID uint, fromYear int) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i, existing := range r.entitlements {
		if existing.UserID == userID && existing.FromYear == fromYear {
			r.entitlements = slices.Delete(r.entitlements, i, i+1)

			return true, nil
		}
	}

	return false, nil
}
