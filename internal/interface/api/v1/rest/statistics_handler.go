package rest

import (
	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// StatisticsHandler answers what the caller's own time adds up to.
//
// Under /me/ and keyed on the caller, which is what lets it need nothing beyond
// the permission to read your own entries. The project report needs reports:read,
// which both default roles are deliberately without - so an employee could not
// have seen a chart of their own week through it, and it cannot express "every
// project" or "no project" either.
type StatisticsHandler struct {
	statistics *service.StatisticsService
	authz      *Authorizer
	timezone   InstanceTimezoneFunc
}

// NewStatisticsHandler creates the handler.
func NewStatisticsHandler(
	statistics *service.StatisticsService,
	authz *Authorizer,
	timezone InstanceTimezoneFunc,
) *StatisticsHandler {
	return &StatisticsHandler{statistics: statistics, authz: authz, timezone: timezone}
}

// DayTotalResponse is one bar of the day chart.
type DayTotalResponse struct {
	Date  Date    `json:"date"`
	Hours float64 `json:"hours"`
}

// ProjectTotalResponse is one bar of the project chart.
type ProjectTotalResponse struct {
	// ProjectID is null for the entries that have no project yet, which is an
	// answer rather than a gap: "not categorised" is worth seeing the size of.
	ProjectID *uint   `json:"projectId"`
	Name      string  `json:"name"`
	Hours     float64 `json:"hours"`
}

// StatusTotalResponse is how the time is spread across the workflow.
type StatusTotalResponse struct {
	Status string  `json:"status"`
	Hours  float64 `json:"hours"`
}

// StatisticsResponse is everything a chart of your own time needs, in one request.
type StatisticsResponse struct {
	From Date `json:"from"`
	To   Date `json:"to"`

	TotalHours float64 `json:"totalHours"`

	// Days includes the empty ones. A chart drawn only from the days that have
	// entries shows three bars where a week had three working days and four empty
	// ones, which reads as a full week.
	Days []DayTotalResponse `json:"days"`

	Projects []ProjectTotalResponse `json:"projects"`
	Statuses []StatusTotalResponse  `json:"statuses"`
}

// Own handles GET /api/v1/me/statistics.
func (h *StatisticsHandler) Own(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermTimesheetReadOwn)
	if err != nil {
		return nil, err
	}

	// In the reader's own zone, so "this month" is their month: the range would
	// otherwise be a day out for anybody far enough east or west of the server.
	zone := principal.User.TimezoneOf(h.timezone.resolve(c))

	from, to, err := overtimeRange(c, zone)
	if err != nil {
		return nil, toHTTPError(err)
	}

	stats, err := h.statistics.Own(c, principal.User.ID, from, to)
	if err != nil {
		return nil, toHTTPError(err)
	}

	response := StatisticsResponse{
		From:       Date{Time: stats.From},
		To:         Date{Time: stats.To},
		TotalHours: stats.TotalHours,
		Days:       make([]DayTotalResponse, 0, len(stats.Days)),
		Projects:   make([]ProjectTotalResponse, 0, len(stats.Projects)),
		Statuses:   make([]StatusTotalResponse, 0, len(stats.Statuses)),
	}

	for _, day := range stats.Days {
		response.Days = append(response.Days,
			DayTotalResponse{Date: Date{Time: day.Date}, Hours: day.Hours})
	}

	for _, project := range stats.Projects {
		response.Projects = append(response.Projects, ProjectTotalResponse{
			ProjectID: project.ProjectID, Name: project.Name, Hours: project.Hours,
		})
	}

	for _, status := range stats.Statuses {
		response.Statuses = append(response.Statuses,
			StatusTotalResponse{Status: status.Status, Hours: status.Hours})
	}

	return response, nil
}
