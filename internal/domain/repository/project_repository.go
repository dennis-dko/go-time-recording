package repository

import (
	"context"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// ProjectRepository repository functions for project
type ProjectRepository interface {
	Save(ctx context.Context, project *model.Project) (*model.Project, error)

	GetByID(ctx context.Context, id uint) (*model.Project, error)

	GetAll(ctx context.Context) ([]*model.Project, error)

	Update(ctx context.Context, project *model.Project) (*model.Project, error)

	Delete(ctx context.Context, id uint) error
}
