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
}

// NewSetupService creates new instance.
func NewSetupService(settings *SettingsService, users repository.UserRepository) *SetupService {
	return &SetupService{settings: settings, users: users}
}

// State reports the wizard's steps and whether it has been dismissed.
func (s *SetupService) State(ctx context.Context) (model.SetupState, error) {
	completed, err := s.settings.SetupCompleted(ctx)
	if err != nil {
		return model.SetupState{}, err
	}

	// The database is deliberately not a step here. It cannot be, because
	// everything below is stored *in* it: choosing one at this point would point
	// the application at an empty database and leave the password change, the
	// timezone and the instance title behind in the old one - including the
	// changed administrator password, so the installation would come back up
	// reachable with the initial password from the documentation.
	//
	// So it is settled before the application starts at all, by the installer
	// package, which is the only place it can be settled honestly. By the time
	// anyone can sign in to see this wizard, that decision has been made.
	steps := []model.SetupStep{
		{ID: model.SetupStepPassword, Required: true, Done: s.passwordChanged(ctx)},
		{ID: model.SetupStepTimezone, Required: true, Done: s.timezoneChosen(ctx)},
		{ID: model.SetupStepBranding, Required: false, Done: s.brandingSet(ctx)},
		{ID: model.SetupStepDirectory, Required: false, Done: s.directoryConfigured(ctx)},
	}

	return model.SetupState{Completed: completed, Steps: steps}, nil
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

func (s *SetupService) brandingSet(ctx context.Context) bool {
	stored, err := s.settings.Raw(ctx, model.SettingAppTitle)

	return err == nil && stored != ""
}

func (s *SetupService) directoryConfigured(ctx context.Context) bool {
	config, err := s.settings.LDAP(ctx)

	return err == nil && config.Enabled && config.Host != ""
}
