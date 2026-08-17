package repository

import (
	"context"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// UserRepository repository functions for user
type UserRepository interface {
	Save(ctx context.Context, user *model.User) (*model.User, error)

	GetByID(ctx context.Context, id uint) (*model.User, error)

	// GetByEmail resolves the login identifier used for authentication.
	GetByEmail(ctx context.Context, email string) (*model.User, error)

	// GetByExternalID resolves a directory-backed account by the identifier
	// the directory assigned it. That identifier outlives a rename, which the
	// mail address does not.
	GetByExternalID(ctx context.Context, externalID string) (*model.User, error)

	GetAll(ctx context.Context) ([]*model.User, error)

	Update(ctx context.Context, user *model.User) (*model.User, error)

	// SetPreference writes one of the settings a person changes in passing, and
	// nothing else.
	//
	// Update writes every column, which is correct for editing an account and wrong
	// for these: read the row, change one field, write it all back, and any change
	// somebody made in between is gone. That is not theoretical - recording the
	// guided tour as seen erased a two-factor secret that had been issued a
	// moment earlier, because both had read the same row.
	//
	// The field is named by the enum rather than by a column name, so a caller
	// cannot reach a column this was not meant to touch.
	SetPreference(ctx context.Context, id uint, field Preference, value string) error

	// SetTOTP writes the second factor's secret and whether it is in force, and
	// nothing else.
	//
	// Both columns in one statement, because they are one fact: a secret with the
	// flag off is a pending enrolment, and the flag on without a secret is an
	// account nobody can sign in to. Two statements could be interleaved into
	// either of those.
	SetTOTP(ctx context.Context, id uint, secret string, enabled bool) error

	Delete(ctx context.Context, id uint) error
}

// Preference names a single-field setting SetPreference may write.
//
// A closed set rather than a column name, so nothing can be reached that was not
// intended - and so the compiler, not a string, decides what is valid.
type Preference uint8

const (
	// PreferenceTourSeen records that the introduction has been offered.
	PreferenceTourSeen Preference = iota

	// PreferenceLanguage is the language the interface is shown in.
	PreferenceLanguage

	// PreferenceTimezone is the zone this person's days are counted in, empty
	// meaning they follow the instance.
	PreferenceTimezone

	// PreferenceTheme is light or dark, empty meaning the time of day decides.
	PreferenceTheme
)
