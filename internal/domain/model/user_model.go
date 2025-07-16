package model

const (
	UserRoleAdmin    = "admin"
	UserRoleManager  = "manager"
	UserRoleEmployee = "employee"
)

// User model
type User struct {
	ID    uint   `json:"id"  sql:"auto_increment"`
	Name  string `json:"name" sql:"not_null"`
	Email string `json:"email" sql:"not_null"`
	Role  string `json:"role" sql:"not_null"`
}

func (u *User) TableName() string {
	return "user"
}
