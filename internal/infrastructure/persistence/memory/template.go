package memory

import (
	"context"
	"slices"
	"sync"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
)

// TemplateRepository is an in-memory repository.TemplateRepository.
type TemplateRepository struct {
	mu        sync.RWMutex
	templates []model.EntryTemplate
	nextID    uint
}

// NewTemplateRepository creates an empty one.
func NewTemplateRepository() *TemplateRepository {
	return &TemplateRepository{nextID: 1}
}

var _ repository.TemplateRepository = (*TemplateRepository)(nil)

func (r *TemplateRepository) ForUser(_ context.Context, userID uint) ([]model.EntryTemplate, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var own []model.EntryTemplate

	for _, template := range r.templates {
		if template.UserID == userID {
			own = append(own, template)
		}
	}

	return own, nil
}

func (r *TemplateRepository) Save(_ context.Context, template model.EntryTemplate) (*model.EntryTemplate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	template.ID = r.nextID
	r.nextID++

	r.templates = append(r.templates, template)

	return &template, nil
}

func (r *TemplateRepository) Delete(_ context.Context, id, userID uint) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i, existing := range r.templates {
		if existing.ID == id && existing.UserID == userID {
			r.templates = slices.Delete(r.templates, i, i+1)

			return true, nil
		}
	}

	return false, nil
}
