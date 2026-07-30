package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/query"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

func TestCreateProjectDefaultsToActive(t *testing.T) {
	f := newFixture(t)

	res, err := f.projects.CreateProject(context.Background(), command.CreateProjectCommand{
		Name: "Fresh", StartDate: day(1),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if res.Result.Status != model.ProjectStatusActive {
		t.Errorf("expected %q, got %q", model.ProjectStatusActive, res.Result.Status)
	}
}

func TestCreateProjectRejectsEndBeforeStart(t *testing.T) {
	f := newFixture(t)

	end := day(1)
	_, err := f.projects.CreateProject(context.Background(), command.CreateProjectCommand{
		Name: "Backwards", StartDate: day(10), EndDate: &end,
	})
	requireKind(t, err, apperror.KindInvalid)
}

func TestArchiveRequiresCompleted(t *testing.T) {
	f := newFixture(t)

	archived := model.ProjectStatusArchived
	_, err := f.projects.UpdateProject(context.Background(),
		command.UpdateProjectCommand{ID: f.projectID, Status: &archived})
	requireKind(t, err, apperror.KindConflict)
}

func TestArchiveBlockedByOpenEntries(t *testing.T) {
	f := newFixture(t)
	f.book(t, day(15), 4) // stays open

	completed := model.ProjectStatusCompleted
	if _, err := f.projects.UpdateProject(context.Background(),
		command.UpdateProjectCommand{ID: f.projectID, Status: &completed}); err != nil {
		t.Fatalf("completing: %v", err)
	}

	archived := model.ProjectStatusArchived
	_, err := f.projects.UpdateProject(context.Background(),
		command.UpdateProjectCommand{ID: f.projectID, Status: &archived})
	requireKind(t, err, apperror.KindConflict)
}

func TestDeleteProjectBlockedByEntries(t *testing.T) {
	f := newFixture(t)
	f.book(t, day(15), 4)

	err := f.projects.DeleteProject(context.Background(),
		command.DeleteProjectCommand{ID: f.projectID})
	requireKind(t, err, apperror.KindConflict)
}

func TestListProjectsFiltersByStatus(t *testing.T) {
	f := newFixture(t)

	if _, err := f.projects.CreateProject(context.Background(), command.CreateProjectCommand{
		Name: "Second", StartDate: day(1), Status: model.ProjectStatusCompleted,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	res, err := f.projects.ListProjects(context.Background(),
		query.ListProjectsQuery{Status: model.ProjectStatusCompleted})
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if res.TotalCount != 1 {
		t.Fatalf("expected 1 completed project, got %d", res.TotalCount)
	}

	if res.Result[0].Name != "Second" {
		t.Errorf("expected \"Second\", got %q", res.Result[0].Name)
	}
}

func TestCreateUserValidation(t *testing.T) {
	f := newFixture(t)

	cases := map[string]command.CreateUserCommand{
		"no name":    {Email: "a@b.de", Role: model.UserRoleAdmin},
		"bad email":  {Name: "A", Email: "not-an-email", Role: model.UserRoleAdmin},
		"bad role":   {Name: "A", Email: "a@b.de", Role: "wizard"},
		"empty role": {Name: "A", Email: "a@b.de"},
	}

	for name, cmd := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := f.users.CreateUser(context.Background(), cmd)
			requireKind(t, err, apperror.KindInvalid)
		})
	}
}

func TestDuplicateEmailIsConflict(t *testing.T) {
	f := newFixture(t)

	_, err := f.users.CreateUser(context.Background(), command.CreateUserCommand{
		Name: "Clone", Email: "dennis@example.com", Role: model.UserRoleEmployee,
	})
	requireKind(t, err, apperror.KindConflict)
}

func TestListUsersPaginates(t *testing.T) {
	f := newFixture(t)

	for _, email := range []string{"b@x.de", "c@x.de", "d@x.de"} {
		if _, err := f.users.CreateUser(context.Background(), command.CreateUserCommand{
			Name: email, Email: email, Role: model.UserRoleEmployee,
		}); err != nil {
			t.Fatalf("seed %s: %v", email, err)
		}
	}

	res, err := f.users.ListUsers(context.Background(), query.ListUsersQuery{Page: 2, Limit: 2})
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if len(res.Result) != 2 {
		t.Errorf("expected 2 users on page 2, got %d", len(res.Result))
	}

	// TotalCount must report the unpaged total so a client can size its pager.
	if res.TotalCount != 4 {
		t.Errorf("expected total 4, got %d", res.TotalCount)
	}
}

func TestReportSumsHoursPerUser(t *testing.T) {
	f := newFixture(t)
	f.book(t, day(15), 4)
	f.book(t, day(16), 2)

	// Outside the range below, so it must not be counted.
	f.book(t, day(28), 5)

	report, err := f.tsDomain.GenerateProjectTimeReport(
		context.Background(), f.projectID, day(14), day(20))
	if err != nil {
		t.Fatalf("report: %v", err)
	}

	if got := report[f.userID]; got != 6 {
		t.Errorf("expected 6h in range, got %v", got)
	}
}

// Guards the assumption that day() builds UTC midnights, which the range
// filtering above depends on.
func TestDayHelperIsUTCMidnight(t *testing.T) {
	d := day(15)
	if d.Location() != time.UTC || d.Hour() != 0 {
		t.Fatalf("day() must return UTC midnight, got %s", d)
	}
}
