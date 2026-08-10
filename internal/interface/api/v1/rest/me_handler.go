package rest

import (
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

// MeHandler serves the endpoints about the signed-in user: identity, password
// and overtime.
type MeHandler struct {
	auth     *service.AuthService
	sessions *service.SessionService
	overtime *service.OvertimeService
	authz    *Authorizer
	timezone InstanceTimezoneFunc
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

	return MeResponse{
		User:        newUserResponseFromModel(principal.User, h.timezone.resolve(c)),
		Permissions: permissions,
		AuthEnabled: h.authz.Enabled(),
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
// Reading someone else's balance needs the wider reporting permission; your
// own only needs the "own" one.
func (h *MeHandler) Overtime(c *gofr.Context) (any, error) {
	id, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	principal, err := h.authz.Principal(c)
	if err != nil {
		return nil, err
	}

	if h.authz.Enabled() {
		// Reading somebody else's balance is the wider permission. Reading your
		// own is the narrower one - which until now was checked nowhere at all,
		// so reports:read:own appeared in the role editor and granted nothing.
		// A permission that exists only in the database is exactly what the
		// comment on the permission list says must not happen.
		// Somebody else's balance is somebody else's recorded time, totalled. It
		// answers the same question the entry list does, so it takes the same right
		// rather than one of its own.
		if principal.User.ID != id && !principal.Can(model.PermTimesheetReadAll) {
			return nil, forbiddenError{msg: "missing permission: " + model.PermTimesheetReadAll}
		}

		if principal.User.ID == id &&
			!principal.Can(model.PermReportReadOwn) &&
			!principal.Can(model.PermTimesheetReadAll) {
			return nil, forbiddenError{msg: "missing permission: " + model.PermReportReadOwn}
		}
	}

	from, to, err := overtimeRange(c, h.locationFor(c, principal.User))
	if err != nil {
		return nil, toHTTPError(err)
	}

	balance, err := h.overtime.Balance(c, id, from, to)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return newOvertimeResponse(balance), nil
}

// their own month. The stored booking dates are calendar days already, so this
// only decides the window, never which day an entry belongs to.
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
		})
	}

	return OvertimeResponse{
		UserID:       b.UserID,
		UserName:     b.UserName,
		From:         Date{Time: b.From},
		To:           Date{Time: b.To},
		DailyTarget:  b.DailyTarget,
		Days:         days,
		TotalBooked:  b.TotalBooked,
		TotalTarget:  b.TotalTarget,
		TotalBalance: b.TotalBalance,
	}
}
