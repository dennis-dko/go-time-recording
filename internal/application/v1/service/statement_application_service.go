package service

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// StatementService works out what somebody's billable hours come to over a
// period: a statement of work, for whoever charges for their time.
//
// Deliberately not an invoice. It has no number, no tax and no customer, because
// each of those is a rule about somebody's books that this application cannot
// know, and an invoice that gets one of them wrong is worse than a figure that
// says what it is - the hours, the rate and what they make, project by project.
// Whoever bills from it writes the invoice their own way.
type StatementService struct {
	timesheets repository.TimesheetRepository
	projects   repository.ProjectRepository
}

// NewStatementService reads the hours and the projects they are charged at.
func NewStatementService(timesheets repository.TimesheetRepository,
	projects repository.ProjectRepository) *StatementService {
	return &StatementService{timesheets: timesheets, projects: projects}
}

// StatementLine is one billable project over a period.
type StatementLine struct {
	ProjectID uint
	Name      string
	Hours     float64
	Currency  string

	// HourlyRate and Amount are in hundredths of Currency, nil where the project
	// has no rate yet: its hours are listed, and what they come to is not guessed.
	HourlyRate *int64
	Amount     *int64
}

// StatementTotal is what the priced lines in one currency come to.
type StatementTotal struct {
	Currency string
	Hours    float64
	Amount   int64
}

// Statement is somebody's billable hours over a period, a line per project and a
// total per currency - never one total across currencies, which would add euros
// to francs.
type Statement struct {
	From   time.Time
	To     time.Time
	Lines  []StatementLine
	Totals []StatementTotal
}

// Statement totals the billable hours somebody booked between two days,
// inclusive, per project of theirs.
//
// Only their own projects: an entry left on somebody else's project from the
// days projects were shared is theirs to read, and not theirs to charge for.
func (s *StatementService) Statement(ctx context.Context, userID uint, from, to time.Time) (*Statement, error) {
	if userID == 0 {
		return nil, apperror.InvalidFields("userId")
	}

	if to.Before(from) {
		return nil, apperror.Invalidf("'to' must not be before 'from'").WithCode("rangeInverted")
	}

	entries, err := s.timesheets.GetByFilter(ctx, repository.TimesheetFilter{
		UserID: userID, StartDate: &from, EndDate: &to,
	})
	if err != nil {
		return nil, err
	}

	perProject := map[uint]float64{}

	for _, entry := range entries {
		if entry.HasProject() {
			perProject[*entry.ProjectID] += entry.DurationHours
		}
	}

	statement := &Statement{From: from, To: to, Lines: []StatementLine{}, Totals: []StatementTotal{}}

	for id, hours := range perProject {
		project, err := s.projects.GetByID(ctx, id)
		if err != nil {
			return nil, err
		}

		if !project.VisibleTo(userID) || !project.Billable {
			continue
		}

		line := StatementLine{
			ProjectID: id, Name: project.Name, Hours: settled(hours),
			Currency: project.Currency, HourlyRate: project.HourlyRate,
		}

		if amount, charged := project.Charge(hours); charged {
			line.Amount = &amount
		}

		statement.Lines = append(statement.Lines, line)
	}

	slices.SortFunc(statement.Lines, func(a, b StatementLine) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.ProjectID, b.ProjectID))
	})

	statement.Totals = totalsPerCurrency(statement.Lines)

	return statement, nil
}

// totalsPerCurrency adds up the priced lines of each currency, in the order of the
// currencies' codes.
func totalsPerCurrency(lines []StatementLine) []StatementTotal {
	totals := []StatementTotal{}

	for _, line := range lines {
		if line.Amount == nil {
			continue
		}

		at := slices.IndexFunc(totals, func(total StatementTotal) bool { return total.Currency == line.Currency })
		if at < 0 {
			totals = append(totals, StatementTotal{Currency: line.Currency})
			at = len(totals) - 1
		}

		totals[at].Hours = settled(totals[at].Hours + line.Hours)
		totals[at].Amount += *line.Amount
	}

	slices.SortFunc(totals, func(a, b StatementTotal) int { return cmp.Compare(a.Currency, b.Currency) })

	return totals
}
