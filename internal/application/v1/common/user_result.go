package common

import "github.com/dennis-dko/go-time-recording/internal/domain/model"

// UserResult result model
type UserResult struct {
	ID    uint
	Name  string
	Email string
	Role  string
}

func NewUserResultFromModel(userModels ...*model.User) []*UserResult {
	if userModels == nil {
		return nil
	}
	var userResult []*UserResult
	for _, userModel := range userModels {
		userData := &UserResult{
			ID:    userModel.ID,
			Name:  userModel.Name,
			Email: userModel.Email,
			Role:  userModel.Role,
		}
		userResult = append(userResult, userData)
	}
	return userResult
}
