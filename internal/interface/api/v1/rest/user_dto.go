package rest

import "github.com/dennis-dko/go-time-recording/internal/domain/model"

// User data transfer object
type User struct {
	model.User
}

// RestPath define user api endpoint
func (u *User) RestPath() string {
	return "api/v1/users"
}
