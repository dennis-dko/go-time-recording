package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/pkg/security"
)

// SecretsService answers one question at start-up: is the configured key the key
// this installation's secrets were written with.
//
// Bringing what an installation already had under that key is a separate job and
// lives with the data, in sqldb.SealStoredSecrets - it has to see the column
// rather than the value, and every read here has already decrypted.
type SecretsService struct {
	settings repository.SettingsRepository
	secrets  *security.Sealer
}

// NewSecretsService creates new instance.
func NewSecretsService(
	settings repository.SettingsRepository,
	secrets *security.Sealer,
) *SecretsService {
	return &SecretsService{settings: settings, secrets: secrets}
}

// theKeyIsRight is what the check value decrypts to. Its content does not matter;
// that it decrypts at all is the whole test.
const theKeyIsRight = "go-time-recording"

// Verify reports whether the configured key is the one this installation's data
// was written with, and records it the first time.
//
// The failure this prevents is the quiet one. A key that is merely *a* valid key
// decrypts nothing that was written with another, and everything that reads a
// secret fails one at a time: a second factor that says the code is wrong, a
// directory that will not bind. Each looks like a different problem, none of them
// looks like the key, and the person diagnosing it is doing so days after
// whatever changed.
//
// So the answer is taken once, at start-up, before anything serves.
func (s *SecretsService) Verify(ctx context.Context) error {
	stored, err := s.settings.Get(ctx, model.SettingSecretKeyCheck)
	if err != nil {
		return err
	}

	if stored == "" {
		// Nothing to check against yet. Recorded only when there is a key, so an
		// installation that never configures one never grows a value it would
		// then be measured against.
		if !s.secrets.Enabled() {
			return nil
		}

		sealed, sealErr := s.secrets.Seal(theKeyIsRight)
		if sealErr != nil {
			return sealErr
		}

		return s.settings.Set(ctx, model.SettingSecretKeyCheck, sealed)
	}

	opened, err := s.secrets.Open(stored)
	if err != nil {
		if errors.Is(err, security.ErrNoKey) {
			return errors.New("this installation's secrets were encrypted and SECRET_KEY " +
				"is not set: put the key back, or clear the second factors and the " +
				"directory password and let them be entered again")
		}

		return fmt.Errorf("SECRET_KEY is not the key this installation's secrets were "+
			"written with: %w", err)
	}

	if opened != theKeyIsRight {
		return errors.New("SECRET_KEY does not match this installation's secrets")
	}

	return nil
}
