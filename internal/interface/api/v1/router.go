package v1

import (
	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/interface/api/v1/rest"
)

// RegisterRoutes register all v1-specified routes
func RegisterRoutes(app *gofr.App) error {
	err := app.AddRESTHandlers(&rest.User{})
	if err != nil {
		return err
	}
	err = app.AddRESTHandlers(&rest.Project{})
	if err != nil {
		return err
	}
	err = app.AddRESTHandlers(&rest.Timesheet{})
	if err != nil {
		return err
	}
	return nil
}
