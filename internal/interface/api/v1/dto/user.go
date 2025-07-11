package dto

// User data transfer object
type User struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

func (u *User) RestPath() string {
	return "api/v1/users"
}
