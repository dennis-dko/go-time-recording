package rest

import "gofr.dev/pkg/gofr"

// GetAll get all users
func (u *User) GetAll(c *gofr.Context) (any, error) {
	return "user GetAll called", nil
}
