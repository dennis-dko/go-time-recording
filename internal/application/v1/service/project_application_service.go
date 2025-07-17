package service

import (
	"context"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/query"
)

// ProjectService service interface
type ProjectService interface {
	CreateProject(ctx context.Context, cmd command.CreateProjectCommand) (*command.CreateProjectCommandResult, error)
	GetProject(ctx context.Context, q query.GetProjectQuery) (*query.GetProjectQueryResult, error)
	ListProjects(ctx context.Context, q query.ListProjectsQuery) (*query.ListProjectsQueryResult, error)
	UpdateProject(ctx context.Context, cmd command.UpdateProjectCommand) (*command.UpdateProjectCommandResult, error)
	DeleteProject(ctx context.Context, cmd command.DeleteProjectCommand) error
}
