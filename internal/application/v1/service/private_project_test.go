package service_test

import (
	"context"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/common"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/query"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

// secondUser adds another account and returns its id.
func secondUser(t *testing.T, f *fixture) uint {
	t.Helper()

	created, err := f.users.CreateUser(context.Background(), command.CreateUserCommand{
		Name: "Erik", Email: "erik@example.com", Role: model.RoleEmployee,
	})
	if err != nil {
		t.Fatalf("create second user: %v", err)
	}

	return created.Result.ID
}

// privateProject creates a personal category for the given owner.
func privateProject(t *testing.T, f *fixture, ownerID uint, name string) uint {
	t.Helper()

	created, err := f.projects.CreateProject(context.Background(), command.CreateProjectCommand{
		Name: name, OwnerID: &ownerID,
	})
	if err != nil {
		t.Fatalf("create private project: %v", err)
	}

	return created.Result.ID
}

// A personal category needs nothing but a name; the start date is filled in.
func TestPrivateProjectNeedsOnlyAName(t *testing.T) {
	f := newFixture(t)

	created, err := f.projects.CreateProject(context.Background(), command.CreateProjectCommand{
		Name: "Meetings", OwnerID: &f.userID,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if created.Result.OwnerID == nil || *created.Result.OwnerID != f.userID {
		t.Fatalf("expected owner %d, got %v", f.userID, created.Result.OwnerID)
	}

	if created.Result.StartDate.IsZero() {
		t.Error("a start date should have been filled in automatically")
	}
}

// The whole point of "private": nobody else may even see it exists.
func TestPrivateProjectIsInvisibleToOthers(t *testing.T) {
	f := newFixture(t)
	otherID := secondUser(t, f)
	projectID := privateProject(t, f, f.userID, "Meetings")

	// The owner sees it.
	own, err := f.projects.ListProjects(context.Background(),
		query.ListProjectsQuery{ViewerID: f.userID})
	if err != nil {
		t.Fatalf("list as owner: %v", err)
	}

	if !containsProject(own.Result, projectID) {
		t.Error("the owner must see their own category")
	}

	// Anyone else does not.
	foreign, err := f.projects.ListProjects(context.Background(),
		query.ListProjectsQuery{ViewerID: otherID})
	if err != nil {
		t.Fatalf("list as other user: %v", err)
	}

	if containsProject(foreign.Result, projectID) {
		t.Error("a private category must not appear for another user")
	}

	// Reading it directly must fail as not-found, so the response does not
	// confirm that the id exists.
	_, err = f.projects.GetProject(context.Background(),
		query.GetProjectQuery{ID: projectID, ViewerID: otherID})
	requireKind(t, err, apperror.KindNotFound)
}

// Nobody else's projects are in your list, and that is the whole list.
//
// This case was the reverse: it asserted that a project with no owner was visible to
// everybody, which is what a shared project was. There are none - a project belongs to
// one person - so the same fixture now proves the opposite, and the list a colleague
// gets is empty rather than containing somebody else's work.
//
// TestOnlyOwnNarrowsToPrivateCategories was here too, checking a parameter that
// narrowed everybody's shared projects down to your own categories. There is nothing
// left to narrow.
func TestSomebodyElsesProjectsAreNotInYourList(t *testing.T) {
	f := newFixture(t)
	otherID := secondUser(t, f)

	result, err := f.projects.ListProjects(context.Background(),
		query.ListProjectsQuery{ViewerID: otherID})
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if containsProject(result.Result, f.projectID) {
		t.Error("a colleague's project is in this user's list")
	}

	if len(result.Result) != 0 {
		t.Errorf("a user who has created nothing sees %d project(s)", len(result.Result))
	}

	// And the owner does see it, so this cannot pass by the list being broken.
	own, err := f.projects.ListProjects(context.Background(),
		query.ListProjectsQuery{ViewerID: f.userID})
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if !containsProject(own.Result, f.projectID) {
		t.Error("the owner cannot see their own project")
	}
}

// Booking onto someone else's category must not be possible, and must not
// reveal that the category exists.
func TestCannotBookOntoAnotherUsersPrivateProject(t *testing.T) {
	f := newFixture(t)
	otherID := secondUser(t, f)
	projectID := privateProject(t, f, f.userID, "Meetings")

	_, err := f.timesheets.CreateTimesheet(context.Background(), command.CreateTimesheetCommand{
		UserID: otherID, ProjectID: projectID, Date: day(15), DurationHours: 2,
	})
	requireKind(t, err, apperror.KindNotFound)

	// The owner can book onto it, which proves the rule is about ownership
	// and not about private projects being unusable.
	if _, err := f.timesheets.CreateTimesheet(context.Background(), command.CreateTimesheetCommand{
		UserID: f.userID, ProjectID: projectID, Date: day(15), DurationHours: 2,
	}); err != nil {
		t.Fatalf("the owner must be able to book onto their own category: %v", err)
	}
}

func TestCannotEditOrDeleteAnotherUsersPrivateProject(t *testing.T) {
	f := newFixture(t)
	otherID := secondUser(t, f)
	projectID := privateProject(t, f, f.userID, "Meetings")

	renamed := "Hijacked"
	_, err := f.projects.UpdateProject(context.Background(), command.UpdateProjectCommand{
		ID: projectID, ActorID: otherID, Name: &renamed,
	})
	requireKind(t, err, apperror.KindNotFound)

	err = f.projects.DeleteProject(context.Background(), command.DeleteProjectCommand{
		ID: projectID, ActorID: otherID,
	})
	requireKind(t, err, apperror.KindNotFound)

	// The owner may do both.
	if _, err := f.projects.UpdateProject(context.Background(), command.UpdateProjectCommand{
		ID: projectID, ActorID: f.userID, Name: &renamed,
	}); err != nil {
		t.Fatalf("the owner must be able to rename their category: %v", err)
	}

	if err := f.projects.DeleteProject(context.Background(), command.DeleteProjectCommand{
		ID: projectID, ActorID: f.userID,
	}); err != nil {
		t.Fatalf("the owner must be able to delete their category: %v", err)
	}
}

// Every default role must be able to keep projects of its own.
//
// The right used to be projects:write:own, beside projects:write for the shared kind.
// One kind of project takes one right, and this is the one it takes.
func TestAllDefaultRolesMayKeepTheirOwnProjects(t *testing.T) {
	f := newFixture(t)

	for _, roleName := range []string{model.RoleAdmin, model.RoleEmployee} {
		role := roleNamed(t, f, roleName)
		if !role.Has(model.PermProjectWrite) {
			t.Errorf("role %q must hold %q", roleName, model.PermProjectWrite)
		}
	}
}

func containsProject(projects []*common.ProjectResult, id uint) bool {
	for _, project := range projects {
		if project.ID == id {
			return true
		}
	}

	return false
}
