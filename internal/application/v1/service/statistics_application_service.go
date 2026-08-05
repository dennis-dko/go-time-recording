package service

import (
	"context"
	"sort"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

// StatisticsService totals somebody's own recorded time, for charting it back to
// them.
//
// Its own service rather than a second use of the project report, and that is a
// decision worth recording. The report is keyed on a project id, needs
// reports:read - which is deliberately withheld from both default roles, so that
// only the built-in administrator can see what other people total up to - and
// cannot express "all projects" or "no project" at all.
//
// None of that fits "show me my own week". This is keyed on the caller and needs
// nothing beyond the permission to read their own entries, which everybody has.
type StatisticsService struct {
	timesheets repository.TimesheetRepository
	projects   repository.ProjectRepository
}

// NewStatisticsService creates new instance.
func NewStatisticsService(
	timesheets repository.TimesheetRepository,
	projects repository.ProjectRepository,
) *StatisticsService {
	return &StatisticsService{timesheets: timesheets, projects: projects}
}

// DayTotal is the hours recorded on one calendar day.
type DayTotal struct {
	Date  time.Time
	Hours float64
}

// ProjectTotal is the hours recorded against one project, or against none.
type ProjectTotal struct {
	// ProjectID is nil for the entries that have no project yet, which is a real
	// answer rather than a missing one: time can be booked first and categorised
	// later, and "not categorised" is worth seeing the size of.
	ProjectID *uint
	Name      string
	Hours     float64
}

// StatusTotal is the hours sitting in one state of the workflow.
type StatusTotal struct {
	Status string
	Hours  float64
}

// OwnStatistics is what somebody's own recorded time adds up to.
type OwnStatistics struct {
	From time.Time
	To   time.Time

	TotalHours float64

	// Days carries every day in the range, including the ones with nothing on
	// them. A bar chart that silently omits empty days draws a week of solid work
	// out of three days and two gaps.
	Days []DayTotal

	Projects []ProjectTotal
	Statuses []StatusTotal
}

// Own totals the caller's entries between two dates, inclusive at both ends.
func (s *StatisticsService) Own(
	ctx context.Context,
	userID uint,
	from, to time.Time,
) (*OwnStatistics, error) {
	if userID == 0 {
		return nil, apperror.InvalidFields("userId")
	}

	if to.Before(from) {
		return nil, apperror.Invalidf("'to' must not be before 'from'")
	}

	entries, err := s.timesheets.GetByFilter(ctx, repository.TimesheetFilter{
		UserID:    userID,
		StartDate: &from,
		EndDate:   &to,
	})
	if err != nil {
		return nil, err
	}

	stats := &OwnStatistics{From: from, To: to}

	perDay := map[string]float64{}
	perProject := map[uint]float64{}
	perStatus := map[string]float64{}

	var withoutProject float64

	for _, entry := range entries {
		// A rejected entry is not work anybody is counting, the same reading the
		// overtime balance takes.
		if entry.Status == model.TimesheetStatusRejected {
			continue
		}

		stats.TotalHours += entry.DurationHours
		perDay[entry.Date.Format(dayKey)] += entry.DurationHours
		perStatus[entry.Status] += entry.DurationHours

		if entry.HasProject() {
			perProject[*entry.ProjectID] += entry.DurationHours
		} else {
			withoutProject += entry.DurationHours
		}
	}

	stats.Days = everyDayInRange(from, to, perDay)

	projects, err := s.projectTotals(ctx, perProject, withoutProject)
	if err != nil {
		return nil, err
	}

	stats.Projects = projects
	stats.Statuses = statusTotals(perStatus)

	return stats, nil
}

// dayKey is the format the day totals are keyed by, which is also how a date
// reaches the wire.
const dayKey = "2006-01-02"

// everyDayInRange fills the gaps.
//
// A chart drawn only from the days that have entries shows three bars where a week
// had three working days and four empty ones - which reads as a full week.
func everyDayInRange(from, to time.Time, totals map[string]float64) []DayTotal {
	days := make([]DayTotal, 0, len(totals)+1)

	// Midnight in the range's own location, so adding a day cannot land on the
	// same day twice across a daylight-saving change.
	day := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, from.Location())
	last := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, to.Location())

	for !day.After(last) {
		days = append(days, DayTotal{Date: day, Hours: totals[day.Format(dayKey)]})
		day = day.AddDate(0, 0, 1)
	}

	return days
}

// projectTotals resolves the names, and puts the uncategorised hours last.
func (s *StatisticsService) projectTotals(
	ctx context.Context,
	perProject map[uint]float64,
	withoutProject float64,
) ([]ProjectTotal, error) {
	totals := make([]ProjectTotal, 0, len(perProject)+1)

	for id, hours := range perProject {
		name := ""

		// A project that has since been deleted leaves entries behind that still
		// count; they are shown without a name rather than dropped.
		if project, err := s.projects.GetByID(ctx, id); err == nil && project != nil {
			name = project.Name
		}

		projectID := id
		totals = append(totals, ProjectTotal{ProjectID: &projectID, Name: name, Hours: hours})
	}

	// Most hours first, which is the order somebody reads a chart in.
	sort.Slice(totals, func(i, j int) bool { return totals[i].Hours > totals[j].Hours })

	// Last on purpose: it is a bucket rather than a project, and it belongs at the
	// end of the list however large it is.
	if withoutProject > 0 {
		totals = append(totals, ProjectTotal{Hours: withoutProject})
	}

	return totals, nil
}

// statusTotals returns the workflow states in their own order rather than in the
// order the entries happened to arrive.
func statusTotals(perStatus map[string]float64) []StatusTotal {
	ordered := []string{
		model.TimesheetStatusOpen,
		model.TimesheetStatusSubmitted,
		model.TimesheetStatusApproved,
	}

	totals := make([]StatusTotal, 0, len(ordered))

	for _, status := range ordered {
		if hours, ok := perStatus[status]; ok {
			totals = append(totals, StatusTotal{Status: status, Hours: hours})
		}
	}

	return totals
}
