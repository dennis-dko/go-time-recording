package rest

import "github.com/dennis-dko/go-time-recording/internal/domain/model"

// Project data transfer object
type Project struct {
	model.Project
}

// RestPath define project api endpoint
func (p *Project) RestPath() string {
	return "api/v1/projects"
}
