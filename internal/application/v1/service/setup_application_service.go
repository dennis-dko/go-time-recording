package service

import (
	"context"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
)

// SetupService reports what a fresh installation still has to settle.
//
// Every answer is derived from what is actually configured rather than from a
// record of what the wizard was shown. That matters because configuration can
// be undone: someone who clears the timezone again has an installation that
// needs the step again, and a stored "step 2 complete" flag would insist
// otherwise.
type SetupService struct {
	settings *SettingsService
	users    repository.UserRepository

	// activeDialect is what this process actually connected to. The stored
	// datasource says what the next restart will use, which is a different
	// question and the wrong one here.
	activeDialect string
}

// NewSetupService creates new instance.
func NewSetupService(
	settings *SettingsService,
	users repository.UserRepository,
	activeDialect string,
) *SetupService {
	return &SetupService{settings: settings, users: users, activeDialect: activeDialect}
}

// State reports the wizard's steps and whether it has been dismissed.
func (s *SetupService) State(ctx context.Context) (model.SetupState, error) {
	completed, err := s.settings.SetupCompleted(ctx)
	if err != nil {
		return model.SetupState{}, err
	}

	steps := []model.SetupStep{
		// First, and required, because everything below is stored *in* the
		// database this step chooses. Switching later points the application at
		// an empty one: the password change, the timezone, the instance title
		// are all left behind in the old database, and start-up recreates the
		// administrator with the initial password from the documentation. An
		// installation that looked configured is then briefly reachable with a
		// password anyone can look up, and nobody expects it because they set a
		// real one minutes earlier.
		//
		// Choosing to stay on SQLite is a legitimate answer and completes the
		// step; what is not acceptable is not having decided.
		{ID: model.SetupStepDatabase, Required: true, Done: s.databaseChosen(ctx)},

		{ID: model.SetupStepPassword, Required: true, Done: s.passwordChanged(ctx)},
		{ID: model.SetupStepTimezone, Required: true, Done: s.timezoneChosen(ctx)},
		{ID: model.SetupStepBranding, Required: false, Done: s.brandingSet(ctx)},
		{ID: model.SetupStepDirectory, Required: false, Done: s.directoryConfigured(ctx)},
	}

	return model.SetupState{Completed: completed, Steps: steps}, nil
}

// KeepDatabase records that the administrator looked at the database question
// and chose to stay on what this process is already running.
//
// Without this the required step could never be completed by anyone who wants
// SQLite, and a required step nobody can complete is just a wizard that never
// goes away.
func (s *SetupService) KeepDatabase(ctx context.Context) error {
	return s.settings.MarkDatasourceChosen(ctx)
}

// Complete records that the wizard was dismissed.
func (s *SetupService) Complete(ctx context.Context) error {
	return s.settings.SetSetupCompleted(ctx, true)
}

// passwordChanged reports whether the built-in administrator still has the
// documented initial password, which is public knowledge.
func (s *SetupService) passwordChanged(ctx context.Context) bool {
	user, err := s.users.GetByEmail(ctx, SystemUserEmail)
	if err != nil {
		// The account is missing, which start-up recreates. Reporting the step
		// as outstanding is the safe reading.
		return false
	}

	return !user.MustChangePassword
}

// timezoneChosen reports whether a zone was picked rather than inherited.
//
// The stored value is read directly instead of going through the resolved
// setting, because the resolved one answers "UTC" both when UTC was chosen and
// when nothing was - and only the second is an outstanding step.
func (s *SetupService) timezoneChosen(ctx context.Context) bool {
	stored, err := s.settings.Raw(ctx, model.SettingTimezone)

	return err == nil && stored != ""
}

// databaseChosen reports whether this process is running on something other
// than the default file database.
func (s *SetupService) databaseChosen(ctx context.Context) bool {
	if s.activeDialect != "" && s.activeDialect != "sqlite" {
		return true
	}

	// A connection saved from the Settings screen counts even while this
	// process still runs on the old one: the decision has been made, and the
	// wizard should not keep asking until the restart.
	stored, err := s.settings.Raw(ctx, model.SettingDatasourceChosen)

	return err == nil && stored != ""
}

func (s *SetupService) brandingSet(ctx context.Context) bool {
	stored, err := s.settings.Raw(ctx, model.SettingAppTitle)

	return err == nil && stored != ""
}

func (s *SetupService) directoryConfigured(ctx context.Context) bool {
	config, err := s.settings.LDAP(ctx)

	return err == nil && config.Enabled && config.Host != ""
}
