package rest

import (
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// OpenIDCallbackPath is where a provider sends the browser back to. Not an API
// route but a page: the interface is served for any path it does not know, and it
// is the interface that hands the code it came back with to Finish.
const OpenIDCallbackPath = "/sign-in/openid"

// openIDCookieName holds a round's state in the browser that began it, so that
// only that browser can finish it.
//
// Without it a code is finished by whoever presents it: somebody who began a
// round, signed in at the provider as themselves and sent the address they came
// back with to somebody else would have that person's browser signed in as them -
// and everything that person then booked would be booked in an account they do
// not own.
const openIDCookieName = "gtr_openid"

// openIDCookiePath confines the cookie to the one request that reads it.
const openIDCookiePath = "/api/v1/auth/openid"

// openIDCookieLifetime is longer than a round may take, so that it is the round
// that expires and says so, rather than the cookie quietly gone first.
const openIDCookieLifetime = 15 * time.Minute

// OpenIDHandler signs people in through the provider the installation configured,
// and lets each link their account to their identity there.
type OpenIDHandler struct {
	openid   *service.OpenIDService
	sessions *service.SessionService
	authz    *Authorizer

	// maintenance and timezone are a sign-in's, as the passkey handler has them:
	// turned away during maintenance as a password sign-in is, and the account in
	// its answer described in the installation's zone.
	maintenance MaintenanceState
	timezone    InstanceTimezoneFunc
}

// NewOpenIDHandler creates the handler.
func NewOpenIDHandler(
	openid *service.OpenIDService,
	sessions *service.SessionService,
	authz *Authorizer,
) *OpenIDHandler {
	return &OpenIDHandler{openid: openid, sessions: sessions, authz: authz}
}

// WithTimezone attaches the installation's zone; see PasskeyHandler.WithTimezone.
func (h *OpenIDHandler) WithTimezone(zone InstanceTimezoneFunc) *OpenIDHandler {
	h.timezone = zone

	return h
}

// WithMaintenance attaches the installation's maintenance state.
func (h *OpenIDHandler) WithMaintenance(state MaintenanceState) *OpenIDHandler {
	h.maintenance = state

	return h
}

// OpenIDOfferResponse says whether the sign-in form offers the provider.
type OpenIDOfferResponse struct {
	Offered bool   `json:"offered"`
	Label   string `json:"label,omitempty"`
}

// OpenIDBeginResponse is where to send the browser.
type OpenIDBeginResponse struct {
	Address string `json:"address"`
}

// OpenIDFinishRequest is what the browser came back from the provider with.
type OpenIDFinishRequest struct {
	State string `json:"state"`
	Code  string `json:"code"`
}

// OpenIDLinkResponse describes the identity an account is linked to.
type OpenIDLinkResponse struct {
	Linked   bool       `json:"linked"`
	Issuer   string     `json:"issuer,omitempty"`
	LinkedAt *time.Time `json:"linkedAt,omitempty"`
}

// Offer handles GET /api/v1/auth/openid, which the sign-in form asks before there
// is anybody to ask it for.
func (h *OpenIDHandler) Offer(c *gofr.Context) (any, error) {
	offered, label, err := h.openid.Offered(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return OpenIDOfferResponse{Offered: offered, Label: label}, nil
}

// BeginSignIn handles POST /api/v1/auth/openid: a sign-in through the provider.
func (h *OpenIDHandler) BeginSignIn(c *gofr.Context) (any, error) {
	address, state, err := h.openid.BeginSignIn(c, openIDRedirectURI(c))
	if err != nil {
		return nil, h.refusal(c, err)
	}

	setCookie(c, openIDCookie(requestOf(c), state, int(openIDCookieLifetime/time.Second)))

	return OpenIDBeginResponse{Address: address}, nil
}

// BeginLink handles POST /api/v1/me/openid: linking the caller's account to their
// identity at the provider, which they can only begin signed in.
func (h *OpenIDHandler) BeginLink(c *gofr.Context) (any, error) {
	principal, err := h.authz.Principal(c)
	if err != nil {
		return nil, err
	}

	// With authentication switched off there is no account to link.
	if principal.User.ID == 0 {
		return nil, unauthorizedError{}
	}

	address, state, err := h.openid.BeginLink(c, principal.User.ID, openIDRedirectURI(c))
	if err != nil {
		return nil, h.refusal(c, err)
	}

	setCookie(c, openIDCookie(requestOf(c), state, int(openIDCookieLifetime/time.Second)))

	return OpenIDBeginResponse{Address: address}, nil
}

// Finish handles POST /api/v1/auth/openid/finish, which the interface sends the
// code and the state the browser came back with: it signs the linked account in,
// or links the account the round was begun for.
func (h *OpenIDHandler) Finish(c *gofr.Context) (any, error) {
	var req OpenIDFinishRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	request := requestOf(c)

	// Whatever happens next, the round is over for this browser.
	setCookie(c, openIDCookie(request, "", -1))

	if !sameRound(request, req.State) {
		return nil, toHTTPError(apperror.Invalidf("this attempt has expired, please try again").
			WithCode("attemptExpired"))
	}

	// From the request as the handler has it, which the session middleware put the
	// caller on - not the raw request kept for cookies, which was taken before there
	// was one.
	var signedIn uint
	if principal, ok := principalFromContext(c.Request.Context()); ok && principal.User != nil {
		signedIn = principal.User.ID
	}

	outcome, err := h.openid.Finish(c, req.State, req.Code, signedIn)
	if err != nil {
		return nil, h.refusal(c, err)
	}

	if outcome.Linked {
		return h.linkResponse(c, outcome.User.ID)
	}

	result, err := h.sessions.OpenSession(c, outcome.User)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return completeSignIn(c, h.sessions, h.maintenance, result, h.timezone.resolve(c))
}

// Linked handles GET /api/v1/me/openid.
func (h *OpenIDHandler) Linked(c *gofr.Context) (any, error) {
	principal, err := h.authz.Principal(c)
	if err != nil {
		return nil, err
	}

	return h.linkResponse(c, principal.User.ID)
}

// Unlink handles DELETE /api/v1/me/openid.
func (h *OpenIDHandler) Unlink(c *gofr.Context) (any, error) {
	principal, err := h.authz.Principal(c)
	if err != nil {
		return nil, err
	}

	if err := h.openid.Unlink(c, principal.User.ID); err != nil {
		return nil, toHTTPError(err)
	}

	return map[string]string{"status": "deleted"}, nil
}

func (h *OpenIDHandler) linkResponse(c *gofr.Context, userID uint) (any, error) {
	identity, err := h.openid.Linked(c, userID)
	if err != nil {
		return nil, toHTTPError(err)
	}

	if identity == nil {
		return OpenIDLinkResponse{}, nil
	}

	return OpenIDLinkResponse{Linked: true, Issuer: identity.Issuer, LinkedAt: momentOrNil(identity.CreatedAt)}, nil
}

// refusal answers a round that could not go on, and writes what the provider said
// underneath to the log: the person is told which of a few things went wrong, and
// the operator reads why.
func (h *OpenIDHandler) refusal(c *gofr.Context, err error) error {
	if detail, ok := apperror.Detail(err); ok && detail.Err != nil && detail.Kind != apperror.KindInternal {
		c.Logger.Warnf("signing in through the provider: %s: %v", detail.Code, detail.Err)
	}

	return toHTTPError(err)
}

// sameRound reports whether the browser finishing a round is the one that began
// it: the state it was handed then, in a cookie only this installation can set.
func sameRound(r *http.Request, state string) bool {
	if r == nil || state == "" {
		return false
	}

	cookie, err := r.Cookie(openIDCookieName)
	if err != nil {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(state)) == 1
}

// openIDCookie carries a round's state for maxAge seconds, or clears it.
//
// Lax rather than Strict, which is what the provider's way back is: a top-level
// navigation from another site, and the request that reads the cookie is made by
// the page that navigation loaded.
func openIDCookie(r *http.Request, state string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name: openIDCookieName, Value: state, Path: openIDCookiePath, MaxAge: maxAge,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: isTLS(r),
	}
}

// openIDRedirectURI is where the provider sends the browser back to: this
// installation as the browser asking reached it.
func openIDRedirectURI(c *gofr.Context) string {
	request := requestOf(c)
	if request == nil {
		return OpenIDCallbackPath
	}

	return requestOrigin(request) + OpenIDCallbackPath
}

// requestOrigin is scheme://host[:port] as the browser that sent a request
// reached this installation, which is what a passkey is bound to and what a
// provider sends the browser back to.
func requestOrigin(r *http.Request) string {
	scheme := "http"
	if isTLS(r) {
		scheme = "https"
	}

	return scheme + "://" + requestHost(r)
}

// addressAsSeen is this installation's address as the page asking sees it, for
// a code another device opens: the Origin its browser sent, which is the page's
// own location.origin, where that names the host the request reached - and
// requestOrigin otherwise, for a request no page made. The page writes the same
// address beside the code, so the two cannot differ even behind a proxy that
// does not say which scheme the browser used.
func addressAsSeen(r *http.Request) string {
	origin, err := url.Parse(r.Header.Get("Origin"))
	if err == nil && (origin.Scheme == "http" || origin.Scheme == "https") &&
		origin.Host != "" && strings.EqualFold(origin.Host, requestHost(r)) {
		return origin.Scheme + "://" + strings.ToLower(origin.Host)
	}

	return requestOrigin(r)
}
