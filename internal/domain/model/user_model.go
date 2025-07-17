package model

const (
	UserRoleAdmin    = "admin"
	UserRoleManager  = "manager"
	UserRoleEmployee = "employee"
)

// User model
type User struct {
	ID    uint
	Name  string
	Email string
	Role  string
}
