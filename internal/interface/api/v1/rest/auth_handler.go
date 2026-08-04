package rest

import (
	"errors"

	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

// AuthHandler serves sign-in, sign-out and two-factor enrolment.
type AuthHandler struct {
	sessions *service.SessionService
	authz    *Authorizer
	issuer   string
	timezone InstanceTimezoneFunc
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

	// The secret is returned as text rather than a QR image: rendering a QR
	// code would mean pulling in a dependency, and every authenticator app
	// also accepts a manually entered secret.
	return map[string]string{"secret": secret, "uri": uri}, nil
}

// ConfirmTOTP handles PUT /api/v1/me/totp, activating the second factor.
func (h *AuthHandler) ConfirmTOTP(c *gofr.Context) (any, error) {
	principal, err := h.authz.Principal(c)
	if err != nil {
		return nil, err
	}

	var req struct {
		Code string `json:"code"`
	}

	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	if err := h.sessions.ConfirmTOTP(c, principal.User.ID, req.Code); err != nil {
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
	principal, err := h.authz.Principal(c)
	if err != nil {
		return nil, err
	}

	var req struct {
		Language string `json:"language"`
	}

	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	if err := h.sessions.SetLanguage(c, principal.User.ID, req.Language); err != nil {
		return nil, toHTTPError(err)
	}

	return map[string]string{"status": "language saved"}, nil
}

// SetTimezone handles PUT /api/v1/me/timezone.
//
// An empty value is meaningful and not an omission: it clears the personal
// setting so the account follows the instance-wide zone again.
func (h *AuthHandler) SetTimezone(c *gofr.Context) (any, error) {
	principal, err := h.authz.Principal(c)
	if err != nil {
		return nil, err
	}

	var req struct {
		Timezone string `json:"timezone"`
	}

	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	if err := h.sessions.SetTimezone(c, principal.User.ID, req.Timezone); err != nil {
		return nil, toHTTPError(err)
	}

	return map[string]string{"status": "timezone saved"}, nil
}

// SetTourSeen handles PUT /api/v1/me/tour.
func (h *AuthHandler) SetTourSeen(c *gofr.Context) (any, error) {
	principal, err := h.authz.Principal(c)
	if err != nil {
		return nil, err
	}

	var req struct {
		Seen bool `json:"seen"`
	}

	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	if err := h.sessions.SetTourSeen(c, principal.User.ID, req.Seen); err != nil {
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
