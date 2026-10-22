package memory

import (
	"context"
	"sync"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
)

// AdminEventRepository is an in-memory repository.AdminEventRepository.
type AdminEventRepository struct {
	mu     sync.RWMutex
	events []model.AdminEvent
}

// NewAdminEventRepository returns an empty log.
func NewAdminEventRepository() *AdminEventRepository {
	return &AdminEventRepository{}
}

var _ repository.AdminEventRepository = (*AdminEventRepository)(nil)

func (r *AdminEventRepository) Record(_ context.Context, event *model.AdminEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	event.ID = uint(len(r.events) + 1)
	r.events = append(r.events, *event)

	return nil
}

func (r *AdminEventRepository) Before(_ context.Context, beforeID uint, limit int) ([]model.AdminEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	page := make([]model.AdminEvent, 0, limit)

	for i := len(r.events) - 1; i >= 0 && len(page) < limit; i-- {
		if beforeID == 0 || r.events[i].ID < beforeID {
			page = append(page, r.events[i])
		}
	}

	return page, nil
}
