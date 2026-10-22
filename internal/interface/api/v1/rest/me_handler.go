package rest

import (
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// MeHandler serves the endpoints about the signed-in user: identity, password
// and overtime.
type MeHandler struct {
	auth     *service.AuthService
	sessions *service.SessionService
	overtime *service.OvertimeService
	authz    *Authorizer
	timezone InstanceTimezoneFunc
	team     *service.TeamService
}

// NewMeHandler creates the handler.
func NewMeHandler(
	auth *service.AuthService,
	sessions *service.SessionService,
	overtime *service.OvertimeService,
	authz *Authorizer,
	timezone InstanceTimezoneFunc,
) *MeHandler {
	return &MeHandler{
		auth: auth, sessions: sessions, overtime: overtime, authz: authz, timezone: timezone,
	}
}

// WithTeam lets /me say whether the installation has managers.
func (h *MeHandler) WithTeam(team *service.TeamService) *MeHandler {
	h.team = team

	return h
}

// Me handles GET /api/v1/me. The UI uses it to decide what to render, so it
// deliberately stays reachable while a password change is still pending.
func (h *MeHandler) Me(c *gofr.Context) (any, error) {
	principal, err := h.authz.Principal(c)
	if err != nil {
		return nil, err
	}

	permissions := principal.Permissions
	if permissions == nil {
		permissions = []string{}
	}

	managers := false
	if h.team != nil {
		if managers, err = h.team.Enabled(c); err != nil {
			return nil, toHTTPError(err)
		}
	}

	return MeResponse{
		User:        newUserResponseFromModel(principal.User, h.timezone.resolve(c)),
		Permissions: permissions,
		AuthEnabled: h.authz.Enabled(),

		// The baseline the interface compares every later response's
		// X-Permissions-Revision against, so it can say that the rights changed
		// rather than only meeting refusals on controls it is still showing.
		PermissionsRevision: PermissionRevision(principal),

		Reminders: RemindersResponse{
			UnbookedDay:     !principal.User.SkipUnbookedReminder,
			TimerAlertHours: principal.User.EffectiveTimerAlertHours(),
		},

		ManagersEnabled: managers,
	}, nil
}

// ChangePassword handles PUT /api/v1/me/password.
func (h *MeHandler) ChangePassword(c *gofr.Context) (any, error) {
	principal, err := h.authz.Principal(c)
	if err != nil {
		return nil, err
	}

	if !h.authz.Enabled() {
		return nil, toHTTPError(apperror.Conflictf(
			"this instance runs without authentication, so there is no password to change").
			WithCode("noAuthNoPassword"))
	}

	var req ChangePasswordRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	err = h.auth.ChangePassword(c, principal.User.ID, req.CurrentPassword, req.NewPassword)
	if err != nil {
		return nil, toHTTPError(err)
	}

	// Every other session is ended, so one opened with the old password on another
	// device stops working immediately - and this one is not, because it is the
	// device that just proved it knew the old password. Ending it as well signed
	// somebody out of the setup wizard between two of its steps.
	var token string
	if cookie, err := requestOf(c).Cookie(SessionCookieName); err == nil {
		token = cookie.Value
	}

	if err := h.sessions.LogoutOthers(c, principal.User.ID, token); err != nil {
		return nil, toHTTPError(err)
	}

	return map[string]string{"status": "password changed"}, nil
}

// Overtime handles GET /api/v1/users/{id}/overtime.
//
// Your own balance takes reports:read:own. Somebody else's is somebody else's
// recorded time, totalled, and nobody reads that - except a manager, for an account
// an administrator assigned to them, and then without the reasons the account was
// away; see service.DaysSeenBy.
func (h *MeHandler) Overtime(c *gofr.Context) (any, error) {
	id, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	principal, err := h.authz.Principal(c)
	if err != nil {
		return nil, err
	}

	account, err := h.authz.accountFor(c, principal, id,
		forbiddenError{msg: "you may only read your own overtime balance"}.WithCode("onlyOwnOvertime"),
		model.PermTimesheetReadTeam)
	if err != nil {
		return nil, err
	}

	// Reading a balance takes the narrow right, which until it was checked here
	// appeared in the role editor and granted nothing.
	if h.authz.Enabled() && !principal.Can(model.PermReportReadOwn) {
		return nil, missingPermission(model.PermReportReadOwn)
	}

	from, to, err := overtimeRange(c, h.locationFor(c, account))
	if err != nil {
		return nil, toHTTPError(err)
	}

	balance, err := h.overtime.Balance(c, account.ID, from, to)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return newOvertimeResponse(balance.SeenBy(principal)), nil
}

// locationFor is the zone this person's dates are read in, so that a report
// asking for "this month" covers their own month.
//
// The stored booking dates are calendar days already, so this only decides the
// window, never which day an entry belongs to.
func (h *MeHandler) locationFor(c *gofr.Context, user *model.User) *time.Location {
	return user.TimezoneOf(h.timezone.resolve(c))
}

// overtimeRange reads the from/to parameters, defaulting to the current month.
//
// "The current month" is a question about a calendar, so it is answered in the
// applicable zone: for someone in Auckland on the first of the month, the
// server's own idea of now is still the previous month.
func overtimeRange(c *gofr.Context, location *time.Location) (from, to time.Time, err error) {
	fromParam, err := queryDate(c, "from")
	if err != nil {
		return from, to, err
	}

	toParam, err := queryDate(c, "to")
	if err != nil {
		return from, to, err
	}

	now := time.Now().In(location)

	to = now
	if toParam != nil {
		to = *toParam
	}

	from = time.Date(to.Year(), to.Month(), 1, 0, 0, 0, 0, location)
	if fromParam != nil {
		from = *fromParam
	}

	return from, to, nil
}

func newOvertimeResponse(b *service.OvertimeBalance) OvertimeResponse {
	days := make([]OvertimeDayResponse, 0, len(b.Days))
	for _, day := range b.Days {
		days = append(days, OvertimeDayResponse{
			Date:    Date{Time: day.Date},
			Booked:  day.Booked,
			Target:  day.Target,
			Balance: day.Balance,
			Absence: day.Absence,
			Holiday: day.Holiday,
		})
	}

	return OvertimeResponse{
		UserID:      b.UserID,
		UserName:    b.UserName,
		From:        Date{Time: b.From},
		To:          Date{Time: b.To},
		DailyTarget: b.DailyTarget,
		Days:        days,

		EveryDayCountsFrom: optionalDate(b.EveryDayCountsFrom),

		TotalBooked:  b.TotalBooked,
		TotalTarget:  b.TotalTarget,
		TotalBalance: b.TotalBalance,
	}
}
