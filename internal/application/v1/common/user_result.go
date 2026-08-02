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
	Language    string
	Timezone    string
	TourSeen    bool

	// Effective working times, with the defaults already applied so callers
	// do not have to know the fallback rules.
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
			Language:           userModel.EffectiveLanguage(),
			Timezone:           userModel.Timezone,
			TourSeen:           userModel.TourSeen,
			DailyTargetHours:   userModel.DailyTargetHours,
			MaxDailyHours:      userModel.MaxDailyHours,
		})
	}

	return userResult
}
