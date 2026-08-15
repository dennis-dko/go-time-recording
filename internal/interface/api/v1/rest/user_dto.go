package rest

import (
	"context"

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

	TOTPEnabled bool `json:"totpEnabled"`

	// Language is this user's own choice; empty means they have never made one.
	// The emptiness is the whole point: the interface adopts the browser's
	// language on a first sign-in, and it can only know it is a first sign-in
	// from a language that is not there. This used to be sent resolved, so every
	// account looked like it had chosen English and the browser was never asked.
	Language string `json:"language"`

	// EffectiveLanguage is what to render in once the fallback has been worked
	// out, so nothing that only wants "which language" has to repeat the rule.
	EffectiveLanguage string `json:"effectiveLanguage"`

	IsExternal bool `json:"isExternal"`

	// Timezone is this user's own choice; empty means they follow the
	// instance-wide setting. The UI needs the difference so its picker can show
	// "follow the instance" rather than pretending the inherited zone was
	// chosen deliberately.
	Timezone string `json:"timezone"`

	// TourSeen tells the interface whether to offer the guided tour. It is on
	// the user rather than in the browser, so someone who has been introduced
	// to the application is not introduced again on their next device.
	TourSeen bool `json:"tourSeen"`

	// EffectiveTimezone is what actually applies once the fallback has been
	// worked out. Every date the client computes - which day "today" is, where
	// a calendar month starts - has to use this, not the browser's own zone.
	EffectiveTimezone string `json:"effectiveTimezone"`
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

	// PermissionsRevision changes exactly when what this account may do changes.
	// Every response carries the current one in a header; this is the baseline to
	// compare it against. See PermissionRevision.
	PermissionsRevision string `json:"permissionsRevision"`
}

func newUserResponse(r *common.UserResult, instanceTimezone string) UserResponse {
	return UserResponse{
		ID:                 r.ID,
		Name:               r.Name,
		Email:              r.Email,
		RoleID:             r.RoleID,
		Role:               r.Role,
		IsSystem:           r.IsSystem,
		MustChangePassword: r.MustChangePassword,
		IsExternal:         r.IsExternal,
		TOTPEnabled:        r.TOTPEnabled,
		Language:           r.Language,
		EffectiveLanguage:  r.EffectiveLanguage,
		Timezone:           r.Timezone,
		TourSeen:           r.TourSeen,
		EffectiveTimezone:  model.EffectiveTimezoneName(r.Timezone, instanceTimezone),
		DailyTargetHours:   r.DailyTargetHours,
		MaxDailyHours:      r.MaxDailyHours,
	}
}

// InstanceTimezoneFunc reports the instance-wide zone.
//
// A function rather than the settings service itself, so the handlers that only
// need this one value do not gain the ability to rewrite every setting. It
// returns a plain string because a failure here has an obvious right answer -
// the default - and must never fail the request it is decorating.
type InstanceTimezoneFunc func(ctx context.Context) string

// resolve is nil-safe, so a handler built without one still answers sensibly.
func (f InstanceTimezoneFunc) resolve(ctx context.Context) string {
	if f == nil {
		return model.DefaultTimezone
	}

	return f(ctx)
}

// newUserResponseFromModel converts a domain user, for the handlers that call
// a domain service directly.
func newUserResponseFromModel(u *model.User, instanceTimezone string) UserResponse {
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
		Language:           u.Language,
		EffectiveLanguage:  u.EffectiveLanguage(),
		IsExternal:         u.IsExternal,
		Timezone:           u.Timezone,
		TourSeen:           u.TourSeen,
		EffectiveTimezone:  model.EffectiveTimezoneName(u.Timezone, instanceTimezone),
	}
}

func newUserResponses(results []*common.UserResult, instanceTimezone string) []UserResponse {
	out := make([]UserResponse, 0, len(results))
	for _, r := range results {
		out = append(out, newUserResponse(r, instanceTimezone))
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

	// IsDefault marks one of the roles a fresh installation is seeded with. Sent
	// so the interface does not offer a delete the server will refuse: these can
	// be edited like any other role and cannot be removed, because they are what
	// every new account and every synchronised one is assigned.
	IsDefault bool `json:"isDefault"`
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
		IsDefault:   r.IsDefault(),
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
