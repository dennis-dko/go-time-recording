// Package common holds the result types that both the command and the query
// paths return, so a project just written and a project just read come back in
// one shape rather than in two that drift apart.
//
// These are not the wire format, and the difference matters when changing one.
// internal/interface/api/v1/rest converts them into its own response types -
// newProjectResponse and the two beside it - before anything is marshalled, so
// a field renamed here is an internal change. The contract a browser already
// reads, the one that is immutable once public, belongs to that package rather
// than to this one.
//
// They are built from internal/domain/model and never the other way round.
package common

import (
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// ProjectResult result model
type ProjectResult struct {
	ID          uint
	Name        string
	Description *string
	StartDate   time.Time
	EndDate     *time.Time
	Status      string

	// OwnerID is set for a private project, which acts as a personal
	// category for its owner only.
	OwnerID *uint
}

func NewProjectResultFromModel(projectModels ...*model.Project) []*ProjectResult {
	if projectModels == nil {
		return nil
	}
	var projectResult []*ProjectResult
	for _, projectModel := range projectModels {
		projectData := &ProjectResult{
			ID:        projectModel.ID,
			Name:      projectModel.Name,
			StartDate: projectModel.StartDate,
			EndDate:   projectModel.EndDate,
			Status:    projectModel.Status,
			OwnerID:   projectModel.OwnerID,
		}
		if projectModel.Description != nil {
			projectData.Description = projectModel.Description
		}
		if projectModel.EndDate != nil {
			projectData.EndDate = projectModel.EndDate
		}
		projectResult = append(projectResult, projectData)
	}
	return projectResult
}
