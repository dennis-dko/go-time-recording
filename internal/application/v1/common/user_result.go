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
			DailyTargetHours:   userModel.DailyTargetHours,
			MaxDailyHours:      userModel.MaxDailyHours,
		})
	}

	return userResult
}
