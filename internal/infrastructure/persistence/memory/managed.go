package memory

import (
	"context"
	"slices"
	"sync"

	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
)

// ManagedAccountRepository is an in-memory repository.ManagedAccountRepository.
type ManagedAccountRepository struct {
	mu       sync.RWMutex
	managers map[uint][]uint
}

// NewManagedAccountRepository returns a store with no assignments.
func NewManagedAccountRepository() *ManagedAccountRepository {
	return &ManagedAccountRepository{managers: map[uint][]uint{}}
}

var _ repository.ManagedAccountRepository = (*ManagedAccountRepository)(nil)

func (r *ManagedAccountRepository) ManagersOf(_ context.Context, userID uint) ([]uint, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return append([]uint{}, r.managers[userID]...), nil
}

func (r *ManagedAccountRepository) MembersOf(_ context.Context, managerID uint) ([]uint, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	members := []uint{}

	for userID, managers := range r.managers {
		if slices.Contains(managers, managerID) {
			members = append(members, userID)
		}
	}

	slices.Sort(members)

	return members, nil
}

func (r *ManagedAccountRepository) SetManagers(_ context.Context, userID uint, managerIDs []uint) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	kept := slices.Clone(managerIDs)
	slices.Sort(kept)

	if len(kept) == 0 {
		delete(r.managers, userID)
	} else {
		r.managers[userID] = kept
	}

	return nil
}
