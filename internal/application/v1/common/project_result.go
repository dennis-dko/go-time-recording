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

	// BudgetHours is how many hours its owner means to spend on it, nil for none,
	// and BookedHours how many of their own they have booked on it so far.
	BudgetHours *float64
	BookedHours float64

	// Billable, HourlyRate and Currency are what its hours are charged at, the
	// rate in whole units of the currency and nil where there is none yet.
	Billable   bool
	HourlyRate *float64
	Currency   string
}

// UnitsOf is money kept in hundredths of its currency - a rate, an amount - in
// whole units, nil for none.
func UnitsOf(hundredths *int64) *float64 {
	if hundredths == nil {
		return nil
	}

	rate := float64(*hundredths) / 100

	return &rate
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

			BudgetHours: projectModel.BudgetHours,

			Billable:   projectModel.Billable,
			HourlyRate: UnitsOf(projectModel.HourlyRate),
			Currency:   projectModel.Currency,
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
