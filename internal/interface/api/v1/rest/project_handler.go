package rest

import (
	"gofr.dev/pkg/gofr"
)

// GetAll get all projects
func (p *Project) GetAll(c *gofr.Context) (any, error) {
	return "project GetAll called", nil
}
