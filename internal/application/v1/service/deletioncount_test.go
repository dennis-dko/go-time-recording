package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/memory"
)

// loadCounting counts how often somebody's entries are loaded, as opposed to
// counted.
type loadCounting struct {
	repository.TimesheetRepository

	loaded int
}

func (l *loadCounting) GetByFilter(
	ctx context.Context, filter repository.TimesheetFilter,
) ([]*model.Timesheet, error) {
	l.loaded++

	return l.TimesheetRepository.GetByFilter(ctx, filter)
}

// Deleting a project or an account asks how many entries hang on it, and asks
// for the number rather than for the entries.
//
// Both loaded every one of them to take the length of the list: for an account,
// everything its owner ever recorded. Measured on SQLite with 12,500 entries -
// ten years of one person's time - loading them took 22.7 ms and 6.5 MB, and
// counting them 1.5 ms. The refusal they produce needs the number and nothing
// else.
func TestADeletionCountsTheEntriesItWouldTakeRatherThanLoadingThem(t *testing.T) {
	ctx := context.Background()

	users := memory.NewUserRepository()
	roles := memory.NewRoleRepository(users)
	entries := &loadCounting{TimesheetRepository: memory.NewTimesheetRepository()}

	accounts := service.NewUserApplicationService(users, roles, entries,
		&memoryPurger{users: users, timesheets: entries})
	projects := service.NewProjectApplicationService(memory.NewProjectRepository(), entries,
		memory.NewTimerRepository())

	person, err := accounts.CreateUser(ctx, command.CreateUserCommand{
		Name: "Ada", Email: "ada@example.com", Role: model.RoleUser,
	})
	if err != nil {
		t.Fatalf("creating the account: %v", err)
	}

	owner := person.Result.ID

	project, err := projects.CreateProject(ctx, command.CreateProjectCommand{
		Name: "Website", StartDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), OwnerID: &owner,
	})
	if err != nil {
		t.Fatalf("creating the project: %v", err)
	}

	projectID := project.Result.ID

	if _, err := entries.Save(ctx, &model.Timesheet{
		UserID: owner, ProjectID: &projectID, DurationHours: 2,
		Date: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("booking: %v", err)
	}

	if err := projects.DeleteProject(ctx, command.DeleteProjectCommand{ID: projectID, ActorID: owner}); !hasCode(err, "projectHasEntries") {
		t.Errorf("deleting a project with an entry answered %v, want it refused for its entries", err)
	}

	if err := accounts.DeleteUser(ctx, command.DeleteUserCommand{ID: owner}); !hasCode(err, "deletionNeedsConfirming") {
		t.Errorf("deleting an account with an entry answered %v, want it to ask first", err)
	}

	if entries.loaded != 0 {
		t.Errorf("the two refusals loaded the entries %d time(s) to count them", entries.loaded)
	}
}
