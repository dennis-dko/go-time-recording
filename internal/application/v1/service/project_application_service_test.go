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

// An end date that was set can be taken off again.
//
// Every optional field in this command is a pointer, and a nil one means "leave
// it alone" - which is right for a partial update and leaves no way at all to say
// "there is no end any more". A project that got an end date by mistake, or one
// that turned out to be ongoing after all, was stuck with it: sending an empty
// date is a zero time, and a zero time is before every start date there is, so
// the request came back as an invalid endDate.
//
// So clearing is said separately from setting, and the two cannot be confused
// with each other or with silence.
func TestAnEndDateCanBeClearedAndSilenceStillLeavesItAlone(t *testing.T) {
	f := newFixture(t)

	end := day(20)

	if _, err := f.projects.UpdateProject(context.Background(),
		command.UpdateProjectCommand{ID: f.projectID, EndDate: &end}); err != nil {
		t.Fatalf("set an end: %v", err)
	}

	// Silence leaves it: this is what every other caller sends, the spreadsheet
	// import among them, and it must go on meaning "unchanged".
	name := "Website again"

	after, err := f.projects.UpdateProject(context.Background(),
		command.UpdateProjectCommand{ID: f.projectID, Name: &name})
	if err != nil {
		t.Fatalf("rename: %v", err)
	}

	if after.Result.EndDate == nil {
		t.Fatal("a change that said nothing about the end date removed it")
	}

	cleared, err := f.projects.UpdateProject(context.Background(),
		command.UpdateProjectCommand{ID: f.projectID, ClearEndDate: true})
	if err != nil {
		t.Fatalf("clear the end: %v", err)
	}

	if cleared.Result.EndDate != nil {
		t.Errorf("the end date is still %v, so a project cannot be made open-ended again",
			*cleared.Result.EndDate)
	}
}

// And a description emptied is a description gone, not an empty one.
//
// The difference shows on screen: the table writes a dash where a project has no
// description, and an empty string is not nothing - it is a value, so the cell
// came out blank and read as a column that had broken.
func TestAnEmptiedDescriptionIsNoDescription(t *testing.T) {
	f := newFixture(t)

	written := "Everything about the site"

	if _, err := f.projects.UpdateProject(context.Background(),
		command.UpdateProjectCommand{ID: f.projectID, Description: &written}); err != nil {
		t.Fatalf("describe: %v", err)
	}

	empty := ""

	cleared, err := f.projects.UpdateProject(context.Background(),
		command.UpdateProjectCommand{ID: f.projectID, Description: &empty})
	if err != nil {
		t.Fatalf("empty the description: %v", err)
	}

	if cleared.Result.Description != nil {
		t.Errorf("the description is %q rather than absent", *cleared.Result.Description)
	}
}

func TestArchiveRequiresCompleted(t *testing.T) {
	f := newFixture(t)

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
		"no name":   {Email: "a@b.de", Role: model.RoleAdmin},
		"bad email": {Name: "A", Email: "not-an-email", Role: model.RoleAdmin},
		"bad role":  {Name: "A", Email: "a@b.de", Role: "wizard"},
	}

	for name, cmd := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := f.users.CreateUser(context.Background(), cmd)
			requireKind(t, err, apperror.KindInvalid)
		})
	}
}

// Omitting the role must not fail but land on the least privileged role, so a
// new account can never accidentally be created with elevated rights.
func TestCreateUserWithoutRoleFallsBackToTheEverydayOne(t *testing.T) {
	f := newFixture(t)

	created, err := f.users.CreateUser(context.Background(), command.CreateUserCommand{
		Name: "No Role", Email: "norole@example.com",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if created.Result.Role != model.RoleUser {
		t.Errorf("expected fallback to %q, got %q", model.RoleUser, created.Result.Role)
	}
}

func TestDuplicateEmailIsConflict(t *testing.T) {
	f := newFixture(t)

	_, err := f.users.CreateUser(context.Background(), command.CreateUserCommand{
		Name: "Clone", Email: "dennis@example.com", Role: model.RoleUser,
	})
	requireKind(t, err, apperror.KindConflict)
}

func TestListUsersPaginates(t *testing.T) {
	f := newFixture(t)

	for _, email := range []string{"b@x.de", "c@x.de", "d@x.de"} {
		if _, err := f.users.CreateUser(context.Background(), command.CreateUserCommand{
			Name: email, Email: email, Role: model.RoleUser,
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
		context.Background(), f.projectID, day(14), day(20), 0, 0)
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

// A project that was given no start date starts today, in the shape every other
// date is stored in.
//
// It used to default to midnight in whatever zone the server runs in, while a
// posted "2026-07-03" parses to midnight UTC. The same field then held two
// different things, and on a server an hour or two ahead of UTC the defaulted
// one is the previous day the moment a driver normalises it away.
func TestADefaultedStartDateIsTheSameShapeAsAPostedOne(t *testing.T) {
	f := newFixture(t)
	owner := f.userID

	res, err := f.projects.CreateProject(context.Background(), command.CreateProjectCommand{
		Name: "No date given", OwnerID: &owner,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	start := res.Result.StartDate

	if start.Location() != time.UTC {
		t.Errorf("the defaulted start date is in %s, not UTC - a posted one is "+
			"parsed as UTC, and the two have to be the same thing",
			start.Location())
	}

	if h, m, s := start.Clock(); h != 0 || m != 0 || s != 0 {
		t.Errorf("the defaulted start date carries a time of day (%02d:%02d:%02d); "+
			"a date answers which day and nothing else", h, m, s)
	}

	if got, want := start.Format(time.DateOnly), model.CalendarDay(time.Now()).Format(time.DateOnly); got != want {
		t.Errorf("a project created today starts on %s, want %s", got, want)
	}
}
