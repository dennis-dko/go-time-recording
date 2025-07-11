package v1

import (
	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/interface/api/v1/dto"
)

// RegisterRoutes register all v1-specified routes
func RegisterRoutes(app *gofr.App) error {
	err := app.AddRESTHandlers(&dto.User{})
	if err != nil {
		return err
	}
	err = app.AddRESTHandlers(&dto.Project{})
	if err != nil {
		return err
	}
	err = app.AddRESTHandlers(&dto.Timesheet{})
	if err != nil {
		return err
	}
	return nil
}
