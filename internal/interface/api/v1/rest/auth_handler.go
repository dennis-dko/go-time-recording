package rest

import (
	"errors"

	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
	"github.com/dennis-dko/go-time-recording/internal/pkg/qrcode"
)

// AuthHandler serves sign-in, sign-out and two-factor enrolment.
type AuthHandler struct {
	sessions *service.SessionService
	authz    *Authorizer
	issuer   string
	timezone InstanceTimezoneFunc

	// maintenance is what the installation is doing, if anything. Nil on an
	// installation that has no maintenance state to read, which is the same as
	// not being out of service.
	maintenance MaintenanceState
}

// WithMaintenance lets sign-in know the installation is out of service.
//
// The middleware in front of this cannot decide it. Maintenance mode turns away
// everybody except whoever may administer the installation, and which of those
// two a request is cannot be known until the credentials in it have been
// checked - which is what this handler does. So /auth/login is exempt from the
// middleware, and the decision is made here instead, once there is somebody to
// make it about.
func (h *AuthHandler) WithMaintenance(state MaintenanceState) *AuthHandler {
	h.maintenance = state

	return h
}

// NewAuthHandler creates the handler. issuer is the name authenticator apps
// display next to the account.
func NewAuthHandler(
	sessions *service.SessionService,
	authz *Authorizer,
	issuer string,
	timezone InstanceTimezoneFunc,
) *AuthHandler {
	return &AuthHandler{sessions: sessions, authz: authz, issuer: issuer, timezone: timezone}
}

// LoginRequest is the sign-in payload.
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`

	// TOTP is only needed when the account has a second factor.
	TOTP string `json:"totp"`
}

// LoginResponse reports the outcome of a sign-in attempt.
type LoginResponse struct {
	// Pointers so a challenge response carries no hollow user object that a
	// client could mistake for a real account.
	User        *UserResponse `json:"user,omitempty"`
	Permissions []string      `json:"permissions,omitempty"`

	// TOTPRequired tells the client to ask for the code and retry, rather
	// than reporting the sign-in as failed.
	TOTPRequired bool `json:"totpRequired"`
}

// Login handles POST /api/v1/auth/login.
func (h *AuthHandler) Login(c *gofr.Context) (any, error) {
	var req LoginRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	result, err := h.sessions.Login(c, req.Email, req.Password, req.TOTP)
	if err != nil {
		// A missing second factor is not a failure; the client has to be told
		// to collect the code.
		if errors.Is(err, service.ErrTOTPRequired) {
			return LoginResponse{TOTPRequired: true}, nil
		}

		// The answer stays deliberately vague - somebody who is not signed in
		// learns nothing about why - but the reason must not vanish with it.
		// Without this line an unreachable directory, a database that has gone
		// away and a mistyped password are the same event to whoever has to work
		// out why nobody can sign in: a 401 and nothing else.
		//
		// Only the internal ones. Wrong credentials are the ordinary case and
		// would bury the rest under every typo anybody makes.
		if apperror.KindOf(err) == apperror.KindInternal {
			c.Logger.Errorf("sign-in for %q could not be completed: %v", req.Email, err)
		}

		return nil, unauthorizedError{}
	}

	// Out of service, and not for somebody who could end it.
	//
	// After the credentials are checked rather than before, because "who is
	// this" is the question being answered. It costs a session that is created
	// and immediately ended, which is the price of not having a second way to
	// resolve an account - one that would be a second answer to "is this
	// password right", kept in step with the first by nothing.
	//
	// Ended rather than left to expire: an unused session is still a session,
	// and one handed out during maintenance would let its holder back in the
	// moment maintenance ended, without signing in.
	if turnedAway, notice := h.refusedByMaintenance(c, result.Principal); turnedAway {
		if err := h.sessions.Logout(c, result.Token); err != nil {
			c.Logger.Errorf("could not end the session refused by maintenance: %v", err)
		}

		return nil, notice
	}

	request := requestOf(c)
	setCookie(c, sessionCookie(request, result.Token, result.ExpiresAt))

	// A token handed to an anonymous visitor must not follow them into a
	// signed-in session: if someone else planted the one they arrived with,
	// they would know the value protecting the new session.
	if rotated := RotateCSRFToken(request); rotated != nil {
		setCookie(c, rotated)
	}

	user := newUserResponseFromModel(result.Principal.User, h.timezone.resolve(c))

	return LoginResponse{
		User:        &user,
		Permissions: permissionsOf(result.Principal),
	}, nil
}

// Logout handles POST /api/v1/auth/logout.
func (h *AuthHandler) Logout(c *gofr.Context) (any, error) {
	req := requestOf(c)

	if cookie, err := req.Cookie(SessionCookieName); err == nil {
		if err := h.sessions.Logout(c, cookie.Value); err != nil {
			return nil, toHTTPError(err)
		}
	}

	setCookie(c, expiredCookie(req))

	return map[string]string{"status": "signed out"}, nil
}

// BeginTOTP handles POST /api/v1/me/totp, starting two-factor enrolment.
func (h *AuthHandler) BeginTOTP(c *gofr.Context) (any, error) {
	principal, err := h.authz.Principal(c)
	if err != nil {
		return nil, err
	}

	secret, uri, err := h.sessions.BeginTOTPEnrolment(c, principal.User.ID, h.issuer)
	if err != nil {
		return nil, toHTTPError(err)
	}

	// The secret and the URI stay: an authenticator app takes a typed key, which is
	// the way in on a machine with no camera and the fallback when a code will not
	// scan. The QR code is what everybody else does - reading sixteen characters off
	// one screen into another is where enrolment goes wrong.
	//
	// Rendered here rather than in the browser because the alternative was a QR
	// encoder in JavaScript, and a picture whose only purpose is to be read by a
	// machine is not something to hand-roll on either side.
	response := map[string]string{"secret": secret, "uri": uri}

	// A code that failed to render must not stop the enrolment: the key is on
	// screen either way, and refusing the request would take the working path down
	// with the decorative one.
	if code, err := qrcode.SVGDataURI(uri); err == nil {
		response["qr"] = code
	} else {
		c.Logger.Errorf("could not render the two-factor QR code: %v", err)
	}

	return response, nil
}

// askingWith reads the account behind the request and fills req from the body,
// which is how every personal setting under /me begins.
//
// It was those six lines five times over, character for character - which is
// the state repeated code is in right before it stops being identical, and the
// point at which a fix reaches one copy and not the other four.
//
// The order is the reason this is a function rather than a note asking for it:
// who is asking is settled before the body is read, so a request from nobody is
// refused without decoding anything it sent.
func (h *AuthHandler) askingWith(c *gofr.Context, req any) (uint, error) {
	principal, err := h.authz.Principal(c)
	if err != nil {
		return 0, err
	}

	if err := bind(c, req); err != nil {
		return 0, toHTTPError(err)
	}

	return principal.User.ID, nil
}

// ConfirmTOTP handles PUT /api/v1/me/totp, activating the second factor.
func (h *AuthHandler) ConfirmTOTP(c *gofr.Context) (any, error) {
	var req struct {
		Code string `json:"code"`
	}

	id, err := h.askingWith(c, &req)
	if err != nil {
		return nil, err
	}

	if err := h.sessions.ConfirmTOTP(c, id, req.Code); err != nil {
		return nil, toHTTPError(err)
	}

	return map[string]string{"status": "two-factor authentication enabled"}, nil
}

// DisableTOTP handles DELETE /api/v1/me/totp.
func (h *AuthHandler) DisableTOTP(c *gofr.Context) (any, error) {
	principal, err := h.authz.Principal(c)
	if err != nil {
		return nil, err
	}

	// The code travels as a parameter because a DELETE body is not reliably
	// forwarded by proxies.
	code := c.Param("code")
	if code == "" {
		return nil, toHTTPError(apperror.InvalidFields("code"))
	}

	if err := h.sessions.DisableTOTP(c, principal.User.ID, code); err != nil {
		return nil, toHTTPError(err)
	}

	return map[string]string{"status": "two-factor authentication disabled"}, nil
}

// SetLanguage handles PUT /api/v1/me/language.
func (h *AuthHandler) SetLanguage(c *gofr.Context) (any, error) {
	var req struct {
		Language string `json:"language"`
	}

	id, err := h.askingWith(c, &req)
	if err != nil {
		return nil, err
	}

	if err := h.sessions.SetLanguage(c, id, req.Language); err != nil {
		return nil, toHTTPError(err)
	}

	return map[string]string{"status": "language saved"}, nil
}

// SetTheme handles PUT /api/v1/me/theme.
//
// An empty value is meaningful and not an omission: it clears the choice so the
// screen follows the time of day again.
func (h *AuthHandler) SetTheme(c *gofr.Context) (any, error) {
	var req struct {
		Theme string `json:"theme"`
	}

	id, err := h.askingWith(c, &req)
	if err != nil {
		return nil, err
	}

	if err := h.sessions.SetTheme(c, id, req.Theme); err != nil {
		return nil, toHTTPError(err)
	}

	return map[string]string{"status": "appearance saved"}, nil
}

// SetTimezone handles PUT /api/v1/me/timezone.
//
// An empty value is meaningful and not an omission: it clears the personal
// setting so the account follows the instance-wide zone again.
func (h *AuthHandler) SetTimezone(c *gofr.Context) (any, error) {
	var req struct {
		Timezone string `json:"timezone"`
	}

	id, err := h.askingWith(c, &req)
	if err != nil {
		return nil, err
	}

	if err := h.sessions.SetTimezone(c, id, req.Timezone); err != nil {
		return nil, toHTTPError(err)
	}

	return map[string]string{"status": "timezone saved"}, nil
}

// SetTourSeen handles PUT /api/v1/me/tour.
func (h *AuthHandler) SetTourSeen(c *gofr.Context) (any, error) {
	var req struct {
		Seen bool `json:"seen"`
	}

	id, err := h.askingWith(c, &req)
	if err != nil {
		return nil, err
	}

	if err := h.sessions.SetTourSeen(c, id, req.Seen); err != nil {
		return nil, toHTTPError(err)
	}

	return map[string]any{"tourSeen": req.Seen}, nil
}

// Languages handles GET /api/v1/languages, so the UI offers exactly the
// languages that have translations.
func (*AuthHandler) Languages(_ *gofr.Context) (any, error) {
	return map[string][]string{"languages": model.SupportedLanguages()}, nil
}

func permissionsOf(principal *service.Principal) []string {
	if principal.Permissions == nil {
		return []string{}
	}

	return principal.Permissions
}

// refusedByMaintenance reports whether this account is turned away because the
// installation is out of service, and the refusal to send if it is.
//
// The same rule the middleware applies to every other request: the built-in
// account and anybody holding settings:manage get in, because the only way out
// of maintenance mode is through a screen they are the only ones who can reach.
// Everybody else is told why, which is the part that was missing - sign-in was
// exempt as a whole, so an ordinary account signed in successfully and then met
// a wall of 503s on a screen that had already welcomed them.
func (h *AuthHandler) refusedByMaintenance(
	c *gofr.Context, principal *service.Principal,
) (bool, error) {
	if h.maintenance == nil || principal == nil || principal.User == nil {
		return false, nil
	}

	state := h.maintenance.State(c)
	if !state.Enabled {
		return false, nil
	}

	if principal.User.IsSystem || principal.Can(model.PermSettingsManage) {
		return false, nil
	}

	return true, maintenanceError{state: state}
}
