package common

import "github.com/dennis-dko/go-time-recording/internal/domain/model"

// UserResult result model. It deliberately carries no password material, so a
// hash cannot leak by being passed outwards.
type UserResult struct {
	ID     uint
	Name   string
	Email  string
	RoleID uint
	Role   string

	IsSystem           bool
	MustChangePassword bool

	// IsExternal marks an account backed by the directory. The staff list is
	// where an administrator decides who to edit or remove, and a directory
	// account shown as local invites both - the password cannot be changed here
	// and a deletion is undone by the next synchronisation.
	IsExternal bool

	TOTPEnabled bool

	// Language is the choice as stored, empty for an account that never made
	// one; EffectiveLanguage is what actually applies once the fallback has been
	// worked out. Both travel for the same reason the timezone does: a resolved
	// value alone cannot be told apart from a deliberate "English", and the
	// interface adopts the browser's language exactly once - on a first sign-in,
	// which is precisely the case that used to arrive here already flattened to
	// "en" and so never happened at all.
	Language          string
	EffectiveLanguage string

	Timezone string
	TourSeen bool

	// Working times as this account has them, which is zero for one that has not
	// chosen: zero means "follow the instance default", and the interface shows it as
	// "default" rather than as no hours at all.
	//
	// Deliberately not resolved here, though the comment used to claim it was. A
	// resolved value cannot be told apart from a deliberate one, so somebody who set
	// eight hours and somebody who set nothing would look identical - and the screen
	// would offer to clear a figure that was never there. What reads them resolves
	// them: the overtime balance through EffectiveDailyTarget, the daily ceiling
	// through the stricter of this and the installation's.
	DailyTargetHours float64
	MaxDailyHours    float64
}

func NewUserResultFromModel(userModels ...*model.User) []*UserResult {
	if userModels == nil {
		return nil
	}

	userResult := make([]*UserResult, 0, len(userModels))

	for _, userModel := range userModels {
		userResult = append(userResult, &UserResult{
			ID:                 userModel.ID,
			Name:               userModel.Name,
			Email:              userModel.Email,
			RoleID:             userModel.RoleID,
			Role:               userModel.RoleName,
			IsSystem:           userModel.IsSystem,
			MustChangePassword: userModel.MustChangePassword,
			IsExternal:         userModel.IsExternal,
			TOTPEnabled:        userModel.TOTPEnabled,
			Language:           userModel.Language,
			EffectiveLanguage:  userModel.EffectiveLanguage(),
			Timezone:           userModel.Timezone,
			TourSeen:           userModel.TourSeen,
			DailyTargetHours:   userModel.DailyTargetHours,
			MaxDailyHours:      userModel.MaxDailyHours,
		})
	}

	return userResult
}
