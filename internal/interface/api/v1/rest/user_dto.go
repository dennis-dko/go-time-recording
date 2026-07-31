package rest

import (
	"github.com/dennis-dko/go-time-recording/internal/application/v1/common"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// UserResponse is the wire representation of a user. It carries no password
// material of any kind.
type UserResponse struct {
	ID     uint   `json:"id"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	RoleID uint   `json:"roleId"`
	Role   string `json:"role"`

	IsSystem           bool `json:"isSystem"`
	MustChangePassword bool `json:"mustChangePassword"`

	// Configured values; 0 means "instance default".
	DailyTargetHours float64 `json:"dailyTargetHours"`
	MaxDailyHours    float64 `json:"maxDailyHours"`

	TOTPEnabled bool   `json:"totpEnabled"`
	Language    string `json:"language"`
	IsExternal  bool   `json:"isExternal"`
}

// CreateUserRequest is the payload for creating a user.
type CreateUserRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Role  string `json:"role"`

	// Password may be omitted; the account then starts with the initial
	// password and has to change it before it can be used.
	Password string `json:"password"`

	DailyTargetHours float64 `json:"dailyTargetHours"`
	MaxDailyHours    float64 `json:"maxDailyHours"`
}

// UpdateUserRequest is the payload for a partial update: an omitted field is
// left unchanged, which is why every field is a pointer.
type UpdateUserRequest struct {
	Name  *string `json:"name"`
	Email *string `json:"email"`
	Role  *string `json:"role"`

	DailyTargetHours *float64 `json:"dailyTargetHours"`
	MaxDailyHours    *float64 `json:"maxDailyHours"`
}

// AssignRoleRequest moves a user to another role.
type AssignRoleRequest struct {
	Role string `json:"role"`
}

// WorkingTimesRequest sets a user's daily target and cap.
type WorkingTimesRequest struct {
	DailyTargetHours *float64 `json:"dailyTargetHours"`
	MaxDailyHours    *float64 `json:"maxDailyHours"`
}

// ChangePasswordRequest changes the caller's own password.
type ChangePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

// MeResponse tells the UI who is signed in and what it may show.
type MeResponse struct {
	User        UserResponse `json:"user"`
	Permissions []string     `json:"permissions"`

	// AuthEnabled is false when the instance runs without authentication, so
	// the UI can explain why no sign-in is shown.
	AuthEnabled bool `json:"authEnabled"`
}

func newUserResponse(r *common.UserResult) UserResponse {
	return UserResponse{
		ID:                 r.ID,
		Name:               r.Name,
		Email:              r.Email,
		RoleID:             r.RoleID,
		Role:               r.Role,
		IsSystem:           r.IsSystem,
		MustChangePassword: r.MustChangePassword,
		DailyTargetHours:   r.DailyTargetHours,
		MaxDailyHours:      r.MaxDailyHours,
	}
}

// newUserResponseFromModel converts a domain user, for the handlers that call
// a domain service directly.
func newUserResponseFromModel(u *model.User) UserResponse {
	return UserResponse{
		ID:                 u.ID,
		Name:               u.Name,
		Email:              u.Email,
		RoleID:             u.RoleID,
		Role:               u.RoleName,
		IsSystem:           u.IsSystem,
		MustChangePassword: u.MustChangePassword,
		DailyTargetHours:   u.DailyTargetHours,
		MaxDailyHours:      u.MaxDailyHours,
		TOTPEnabled:        u.TOTPEnabled,
		Language:           u.EffectiveLanguage(),
		IsExternal:         u.IsExternal,
	}
}

func newUserResponses(results []*common.UserResult) []UserResponse {
	out := make([]UserResponse, 0, len(results))
	for _, r := range results {
		out = append(out, newUserResponse(r))
	}

	return out
}

// RoleResponse is the wire representation of a role.
type RoleResponse struct {
	ID          uint     `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	IsSystem    bool     `json:"isSystem"`
	Permissions []string `json:"permissions"`
}

// RoleRequest creates or updates a role.
type RoleRequest struct {
	Name        *string  `json:"name"`
	Description *string  `json:"description"`
	Permissions []string `json:"permissions"`
}

func newRoleResponse(r *model.Role) RoleResponse {
	permissions := r.Permissions
	if permissions == nil {
		permissions = []string{}
	}

	return RoleResponse{
		ID:          r.ID,
		Name:        r.Name,
		Description: r.Description,
		IsSystem:    r.IsSystem,
		Permissions: permissions,
	}
}

// OvertimeDayResponse is one day of an overtime balance.
type OvertimeDayResponse struct {
	Date    Date    `json:"date"`
	Booked  float64 `json:"booked"`
	Target  float64 `json:"target"`
	Balance float64 `json:"balance"`
}

// OvertimeResponse is a user's overtime balance over a period.
type OvertimeResponse struct {
	UserID   uint   `json:"userId"`
	UserName string `json:"userName"`
	From     Date   `json:"from"`
	To       Date   `json:"to"`

	DailyTarget float64               `json:"dailyTarget"`
	Days        []OvertimeDayResponse `json:"days"`

	TotalBooked  float64 `json:"totalBooked"`
	TotalTarget  float64 `json:"totalTarget"`
	TotalBalance float64 `json:"totalBalance"`
}
