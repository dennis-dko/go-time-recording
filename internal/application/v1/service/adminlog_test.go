package service_test

import (
	"context"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/memory"
)

// The log names whoever made a change and whom it was made to as they are called
// now: an account renamed since appears under its new name, and an erased one
// under none at all, though the entry stays - and a role is named by its role. It
// is read newest first, a page at a time, and says when there is more.
func TestTheAdministrationLogNamesPeopleAsTheyAreNow(t *testing.T) {
	f := newFixture(t)
	log := service.NewAdminLogService(memory.NewAdminEventRepository(), f.userRepo, f.roleRepo)

	admin, err := f.users.CreateUser(context.Background(), command.CreateUserCommand{
		Name: "Ada", Email: "ada@example.com", Role: model.RoleUserAdmin,
	})
	if err != nil {
		t.Fatalf("seed the administrator: %v", err)
	}

	gone, err := f.users.CreateUser(context.Background(), command.CreateUserCommand{
		Name: "Gunter", Email: "gunter@example.com", Role: model.RoleUser,
	})
	if err != nil {
		t.Fatalf("seed the account to erase: %v", err)
	}

	role, err := f.roleRepo.GetByName(context.Background(), model.RoleUser)
	if err != nil {
		t.Fatalf("read the ordinary role: %v", err)
	}

	ctx := context.Background()

	for _, change := range []struct {
		action string
		target uint
		detail string
	}{
		{"user.created", gone.Result.ID, ""},
		{"role.updated", role.ID, ""},
		{"user.role", f.userID, model.RoleUserAdmin},
		{"settings.branding", 0, ""},
	} {
		if err := log.Record(ctx, admin.Result.ID, change.action, change.target, change.detail); err != nil {
			t.Fatalf("record %s: %v", change.action, err)
		}
	}

	if err := f.userRepo.Delete(ctx, gone.Result.ID); err != nil {
		t.Fatalf("erase the account: %v", err)
	}

	newest, more, err := log.Page(ctx, 0, 3)
	if err != nil {
		t.Fatalf("read the log: %v", err)
	}

	if len(newest) != 3 || !more {
		t.Fatalf("the first page holds %d entries and more=%v, want 3 and more", len(newest), more)
	}

	if first := newest[0]; first.Action != "settings.branding" || first.ActorName != "Ada" || first.TargetName != "" {
		t.Errorf("the newest entry reads %+v, want Ada changing the branding", first)
	}

	if assigned := newest[1]; assigned.TargetName != "Dennis" || assigned.Detail != model.RoleUserAdmin {
		t.Errorf("the role assignment reads %+v, want Dennis given %s", assigned, model.RoleUserAdmin)
	}

	if changed := newest[2]; changed.TargetName != model.RoleUser {
		t.Errorf("the role change reads %+v, want the role named", changed)
	}

	older, more, err := log.Page(ctx, newest[2].ID, 3)
	if err != nil {
		t.Fatalf("read the next page: %v", err)
	}

	if len(older) != 1 || more {
		t.Fatalf("the next page holds %d entries and more=%v, want the last one and no more", len(older), more)
	}

	if erased := older[0]; erased.Action != "user.created" || erased.TargetID != gone.Result.ID || erased.TargetName != "" {
		t.Errorf("the creation of the erased account reads %+v, want its id kept and no name", erased)
	}
}
