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
