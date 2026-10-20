package service_test

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/memory"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

func ptr[T any](value T) *T { return &value }

// templates builds the service over the fixture's projects.
func (f *fixture) templates() *service.TemplateService {
	return service.NewTemplateService(memory.NewTemplateRepository(), f.projectRepo)
}

// A template is kept once however often it is saved, and listed in the order
// templates were saved.
func TestATemplateIsKeptOnceAndListedInTheOrderSaved(t *testing.T) {
	f := newFixture(t)
	templates := f.templates()
	ctx := context.Background()

	daily := model.EntryTemplate{
		UserID: f.userID, ProjectID: &f.projectID, DurationHours: ptr(0.25), Description: ptr("Daily"),
	}

	first, err := templates.Keep(ctx, daily)
	if err != nil {
		t.Fatalf("keep: %v", err)
	}

	// The same again, its description written with room around it.
	again, err := templates.Keep(ctx, model.EntryTemplate{
		UserID: f.userID, ProjectID: &f.projectID, DurationHours: ptr(0.25), Description: ptr("  Daily "),
	})
	if err != nil {
		t.Fatalf("keep again: %v", err)
	}

	if again.ID != first.ID {
		t.Errorf("the same template saved twice became %d and %d, want one", first.ID, again.ID)
	}

	if _, err := templates.Keep(ctx, model.EntryTemplate{UserID: f.userID, Description: ptr("Review")}); err != nil {
		t.Fatalf("keep a second: %v", err)
	}

	kept, err := templates.Templates(ctx, f.userID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if len(kept) != 2 || *kept[0].Description != "Daily" || *kept[1].Description != "Review" {
		t.Errorf("the templates read %+v, want Daily and then Review", kept)
	}
}

// A template is held to what the booking it fills would be held to.
func TestATemplateIsHeldToWhatABookingIs(t *testing.T) {
	f := newFixture(t)
	templates := f.templates()
	ctx := context.Background()

	for name, template := range map[string]model.EntryTemplate{
		"no hours":        {UserID: f.userID, DurationHours: ptr(0.0)},
		"more than a day": {UserID: f.userID, DurationHours: ptr(24.5)},
		"a description too long": {
			UserID: f.userID, Description: ptr(strings.Repeat("x", model.MaxDescriptionLength+1)),
		},
	} {
		_, err := templates.Keep(ctx, template)
		if apperror.KindOf(err) != apperror.KindInvalid {
			t.Errorf("%s was answered %v, want it refused as invalid", name, err)
		}
	}

	_, err := templates.Keep(ctx, model.EntryTemplate{UserID: f.userID, Description: ptr("   ")})
	requireCode(t, err, "templateEmpty")

	// Somebody else's project is not there to name.
	other, err := f.users.CreateUser(ctx, command.CreateUserCommand{
		Name: "Other", Email: "other@example.com", Role: model.RoleUser,
	})
	if err != nil {
		t.Fatalf("seed a colleague: %v", err)
	}

	theirs, err := f.projects.CreateProject(ctx, command.CreateProjectCommand{
		Name: "Theirs", StartDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), OwnerID: &other.Result.ID,
	})
	if err != nil {
		t.Fatalf("seed their project: %v", err)
	}

	_, err = templates.Keep(ctx, model.EntryTemplate{UserID: f.userID, ProjectID: &theirs.Result.ID})
	requireKind(t, err, apperror.KindNotFound)

	// A finished project takes no bookings, so no template names it.
	completed := model.ProjectStatusCompleted
	if _, err := f.projects.UpdateProject(ctx,
		command.UpdateProjectCommand{ID: f.projectID, Status: &completed}); err != nil {
		t.Fatalf("complete the project: %v", err)
	}

	_, err = templates.Keep(ctx, model.EntryTemplate{UserID: f.userID, ProjectID: &f.projectID})
	requireCode(t, err, "projectClosedForBooking")
}

// Only the person a template belongs to can remove it; to anybody else it is not
// there.
func TestATemplateIsRemovedOnlyByItsOwner(t *testing.T) {
	f := newFixture(t)
	templates := f.templates()
	ctx := context.Background()

	// One kept first, so the template's id and its owner's are not the same
	// number and a call that swapped them could not pass by coincidence.
	if _, err := templates.Keep(ctx, model.EntryTemplate{UserID: f.userID, Description: ptr("Warm-up")}); err != nil {
		t.Fatalf("keep the first: %v", err)
	}

	kept, err := templates.Keep(ctx, model.EntryTemplate{UserID: f.userID, Description: ptr("Daily")})
	if err != nil {
		t.Fatalf("keep: %v", err)
	}

	if kept.ID == f.userID {
		t.Fatalf("the template and its owner are both %d, so this case cannot tell them apart", kept.ID)
	}

	requireKind(t, templates.Remove(ctx, kept.ID, f.userID+1), apperror.KindNotFound)

	if left, _ := templates.Templates(ctx, f.userID); len(left) != 2 {
		t.Fatalf("somebody else's removal left %d template(s), want both", len(left))
	}

	if err := templates.Remove(ctx, kept.ID, f.userID); err != nil {
		t.Fatalf("remove: %v", err)
	}

	requireKind(t, templates.Remove(ctx, kept.ID, f.userID), apperror.KindNotFound)
}

// One person's templates are bounded.
func TestTemplatesAreBounded(t *testing.T) {
	f := newFixture(t)
	templates := f.templates()
	ctx := context.Background()

	for i := range 50 {
		if _, err := templates.Keep(ctx, model.EntryTemplate{
			UserID: f.userID, Description: ptr("task " + strconv.Itoa(i)),
		}); err != nil {
			t.Fatalf("keep template %d: %v", i, err)
		}
	}

	_, err := templates.Keep(ctx, model.EntryTemplate{UserID: f.userID, Description: ptr("one more")})
	requireCode(t, err, "tooManyTemplates")
}
