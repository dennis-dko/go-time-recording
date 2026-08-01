package repository

import (
	"context"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

type TimesheetFilter struct {
	UserID    uint
	ProjectID uint
	Status    string
	StartDate *time.Time
	EndDate   *time.Time

	// WithoutProject selects only the entries that have no project yet.
	// A zero ProjectID means "any project", so a separate flag is needed to
	// ask for the uncategorised ones.
	WithoutProject bool
}

// TimesheetRepository repository functions for timesheet
type TimesheetRepository interface {
	Save(ctx context.Context, timesheet *model.Timesheet) (*model.Timesheet, error)

	GetByID(ctx context.Context, id uint) (*model.Timesheet, error)

	GetByFilter(ctx context.Context, filter TimesheetFilter) ([]*model.Timesheet, error)

	GetAll(ctx context.Context) ([]*model.Timesheet, error)

	Update(ctx context.Context, timesheet *model.Timesheet) (*model.Timesheet, error)

	Delete(ctx context.Context, id uint) error
}
