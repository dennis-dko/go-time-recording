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
			"this instance runs without authentication, so there is no password to change"))
	}

	var req ChangePasswordRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	err = h.auth.ChangePassword(c, principal.User.ID, req.CurrentPassword, req.NewPassword)
	if err != nil {
		return nil, toHTTPError(err)
	}

	// Every existing session is ended, so a session opened with the old
	// password on another device stops working immediately.
	if err := h.sessions.LogoutAll(c, principal.User.ID); err != nil {
		return nil, toHTTPError(err)
	}

	setCookie(c, expiredCookie(requestOf(c)))

	return map[string]string{"status": "password changed, please sign in again"}, nil
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
		if principal.User.ID != id && !principal.Can(model.PermReportRead) {
			return nil, forbiddenError{msg: "missing permission: " + model.PermReportRead}
		}

		if principal.User.ID == id &&
			!principal.Can(model.PermReportReadOwn) && !principal.Can(model.PermReportRead) {
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

// TeamOvertime handles GET /api/v1/overtime, the balance of every user.
func (h *MeHandler) TeamOvertime(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermReportRead)
	if err != nil {
		return nil, err
	}

	from, to, err := overtimeRange(c, h.locationFor(c, principal.User))
	if err != nil {
		return nil, toHTTPError(err)
	}

	balances, err := h.overtime.BalanceForAll(c, from, to)
	if err != nil {
		return nil, toHTTPError(err)
	}

	items := make([]OvertimeResponse, 0, len(balances))
	for _, balance := range balances {
		items = append(items, newOvertimeResponse(balance))
	}

	return listResponse[OvertimeResponse]{Items: items, TotalCount: uint(len(items))}, nil
}

// locationFor returns the zone the caller's calendar questions are answered in.
//
// The caller's rather than the subject's: "this month" is asked by the person
// looking at the screen, and a manager in Berlin reviewing someone abroad means
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
