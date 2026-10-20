package service_test

import (
	"context"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/query"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// projectAsSeen reads a project as its owner sees it.
func (f *fixture) projectAsSeen(t *testing.T) (budget *float64, booked float64) {
	t.Helper()

	got, err := f.projects.GetProject(context.Background(), query.GetProjectQuery{ID: f.projectID, ViewerID: f.userID})
	if err != nil {
		t.Fatalf("read the project: %v", err)
	}

	return got.Result.BudgetHours, got.Result.BookedHours
}

// A budget is set, read beside the hours booked, left alone by an update that says
// nothing about it, and taken off by a budget of nothing.
func TestAProjectKeepsAnHourBudget(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	forty := 40.0

	if _, err := f.projects.UpdateProject(ctx, command.UpdateProjectCommand{
		ID: f.projectID, ActorID: f.userID, BudgetHours: &forty,
	}); err != nil {
		t.Fatalf("set the budget: %v", err)
	}

	f.book(t, day(14), 6.5)
	f.book(t, day(15), 6)

	if budget, booked := f.projectAsSeen(t); budget == nil || *budget != 40 || booked != 12.5 {
		t.Errorf("the project reads a budget of %v with %.2f booked, want 40 with 12.5", budget, booked)
	}

	renamed := "Website relaunch"

	if _, err := f.projects.UpdateProject(ctx, command.UpdateProjectCommand{
		ID: f.projectID, ActorID: f.userID, Name: &renamed,
	}); err != nil {
		t.Fatalf("rename: %v", err)
	}

	if budget, _ := f.projectAsSeen(t); budget == nil || *budget != 40 {
		t.Errorf("an update saying nothing about the budget left %v, want 40", budget)
	}

	nothing := 0.0

	if _, err := f.projects.UpdateProject(ctx, command.UpdateProjectCommand{
		ID: f.projectID, ActorID: f.userID, BudgetHours: &nothing,
	}); err != nil {
		t.Fatalf("take the budget off: %v", err)
	}

	if budget, _ := f.projectAsSeen(t); budget != nil {
		t.Errorf("a budget of nothing left %v, want no budget", *budget)
	}
}

// A budget is a number of hours somebody could spend on a project, or refused.
func TestABudgetThatIsNoNumberOfHoursIsRefused(t *testing.T) {
	f := newFixture(t)

	for _, hours := range []float64{-1, model.MaxBudgetHours + 1} {
		_, err := f.projects.UpdateProject(context.Background(), command.UpdateProjectCommand{
			ID: f.projectID, ActorID: f.userID, BudgetHours: &hours,
		})
		requireKind(t, err, apperror.KindInvalid)
	}
}

// The hours booked are the owner's own: an entry somebody else left on the project
// in the days of shared projects is not counted towards it.
func TestTheBookedHoursAreTheViewersOwn(t *testing.T) {
	f := newFixture(t)

	f.book(t, day(14), 3)

	project := f.projectID

	if _, err := f.timesheetRepo.Save(context.Background(), &model.Timesheet{
		UserID: f.userID + 1, ProjectID: &project, Date: day(15), DurationHours: 5,
	}); err != nil {
		t.Fatalf("leave a colleague's entry on the project: %v", err)
	}

	if _, booked := f.projectAsSeen(t); booked != 3 {
		t.Errorf("the owner sees %.2f booked, want their own three hours", booked)
	}
}
