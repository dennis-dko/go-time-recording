package rest

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/go-webauthn/webauthn/protocol"
	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

// PasskeyHandler serves registration and sign-in with WebAuthn credentials.
type PasskeyHandler struct {
	passkeys *service.PasskeyService
	sessions *service.SessionService
	authz    *Authorizer

	// instanceName is what the device's prompt calls this installation.
	instanceName func(c *gofr.Context) string
}

// NewPasskeyHandler creates the handler.
func NewPasskeyHandler(
	passkeys *service.PasskeyService,
	sessions *service.SessionService,
	authz *Authorizer,
	instanceName func(c *gofr.Context) string,
) *PasskeyHandler {
	return &PasskeyHandler{
		passkeys: passkeys, sessions: sessions, authz: authz, instanceName: instanceName,
	}
}

// relyingParty works out what the browser will bind a credential to.
//
// Derived from the request rather than configured, so an installation needs no
// setup: it works on localhost while developing and on whatever host serves it
// in production, including behind a proxy that rewrote both.
//
// A credential is bound to this name permanently, which is the one thing worth
// knowing about it: moving the application to a different domain makes existing
// passkeys unusable there and everyone re-registers. Nothing else breaks, and
// passwords keep working throughout.
func (h *PasskeyHandler) relyingParty(c *gofr.Context) service.RelyingParty {
	req := requestOf(c)
	if req == nil {
		return service.RelyingParty{}
	}

	host := requestHost(req)

	scheme := "http"
	if isTLS(req) {
		scheme = "https"
	}

	origin := scheme + "://" + host

	// The RP ID is the domain alone. A port in it is rejected by every browser,
	// even though the origin must keep one.
	id := host
	if colon := strings.LastIndex(id, ":"); colon > 0 && !strings.Contains(id, "]") {
		id = id[:colon]
	}

	return service.RelyingParty{ID: id, Origin: origin, DisplayName: h.instanceName(c)}
}

// PasskeyResponse is one registered credential on the wire.
//
// It carries no key material. There would be no harm in it - the public half
// verifies signatures and cannot produce them - but sending it would invite
// somebody to build something on a value that is meaningless outside a
// ceremony.
type PasskeyResponse struct {
	ID         uint    `json:"id"`
	Name       string  `json:"name"`
	CreatedAt  Date    `json:"createdAt"`
	LastUsedAt *Date   `json:"lastUsedAt"`
	Transports string  `json:"transports"`
	BackedUp   bool    `json:"backedUp"`
	SignCount  *uint32 `json:"signCount,omitempty"`
}

func newPasskeyResponse(p *model.Passkey) PasskeyResponse {
	out := PasskeyResponse{
		ID:         p.ID,
		Name:       p.Name,
		CreatedAt:  Date{Time: p.CreatedAt},
		Transports: p.Transports,
		BackedUp:   p.BackedUp,
	}

	if p.LastUsedAt != nil {
		out.LastUsedAt = &Date{Time: *p.LastUsedAt}
	}

	if p.SignCount > 0 {
		count := p.SignCount
		out.SignCount = &count
	}

	return out
}

// Support handles GET /api/v1/auth/passkey, telling the interface whether to
// offer passkeys at all.
//
// Browsers expose WebAuthn only on a secure context - HTTPS, or localhost - so
// on a plain-HTTP deployment the endpoints would work and every browser would
// refuse to call them. Offering a button that cannot work is worse than not
// offering one.
func (h *PasskeyHandler) Support(c *gofr.Context) (any, error) {
	rp := h.relyingParty(c)

	return map[string]any{
		"available": h.passkeys.Available(rp),
		"rpId":      rp.ID,
	}, nil
}

// List handles GET /api/v1/me/passkeys.
func (h *PasskeyHandler) List(c *gofr.Context) (any, error) {
	principal, err := h.authz.Principal(c)
	if err != nil {
		return nil, err
	}

	passkeys, err := h.passkeys.List(c, principal.User.ID)
	if err != nil {
		return nil, toHTTPError(err)
	}

	items := make([]PasskeyResponse, 0, len(passkeys))
	for _, passkey := range passkeys {
		items = append(items, newPasskeyResponse(passkey))
	}

	return listResponse[PasskeyResponse]{Items: items, TotalCount: uint(len(items))}, nil
}

// BeginRegistration handles POST /api/v1/me/passkeys/register.
func (h *PasskeyHandler) BeginRegistration(c *gofr.Context) (any, error) {
	principal, err := h.authz.Principal(c)
	if err != nil {
		return nil, err
	}

	options, token, err := h.passkeys.BeginRegistration(c, principal.User.ID, h.relyingParty(c))
	if err != nil {
		return nil, toHTTPError(err)
	}

	return map[string]any{"options": options, "token": token}, nil
}

// FinishRegistration handles PUT /api/v1/me/passkeys/register.
func (h *PasskeyHandler) FinishRegistration(c *gofr.Context) (any, error) {
	principal, err := h.authz.Principal(c)
	if err != nil {
		return nil, err
	}

	var req struct {
		Token      string         `json:"token"`
		Name       string         `json:"name"`
		Credential map[string]any `json:"credential"`
	}

	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	parsed, err := parseCreation(req.Credential)
	if err != nil {
		return nil, toHTTPError(err)
	}

	passkey, err := h.passkeys.FinishRegistration(c, principal.User.ID,
		h.relyingParty(c), req.Token, req.Name, parsed)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return newPasskeyResponse(passkey), nil
}

// Delete handles DELETE /api/v1/me/passkeys/{id}.
func (h *PasskeyHandler) Delete(c *gofr.Context) (any, error) {
	principal, err := h.authz.Principal(c)
	if err != nil {
		return nil, err
	}

	id, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	if err := h.passkeys.Delete(c, id, principal.User.ID); err != nil {
		return nil, toHTTPError(err)
	}

	return map[string]string{"status": "deleted"}, nil
}

// BeginLogin handles POST /api/v1/auth/passkey/login.
//
// Reachable without a session, which is the point: this is how a session
// begins.
func (h *PasskeyHandler) BeginLogin(c *gofr.Context) (any, error) {
	options, token, err := h.passkeys.BeginLogin(h.relyingParty(c))
	if err != nil {
		return nil, toHTTPError(err)
	}

	return map[string]any{"options": options, "token": token}, nil
}

// FinishLogin handles PUT /api/v1/auth/passkey/login, opening the session.
func (h *PasskeyHandler) FinishLogin(c *gofr.Context) (any, error) {
	var req struct {
		Token      string         `json:"token"`
		Credential map[string]any `json:"credential"`
	}

	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	parsed, err := parseAssertion(req.Credential)
	if err != nil {
		return nil, unauthorizedError{}
	}

	user, err := h.passkeys.FinishLogin(c, h.relyingParty(c), req.Token, parsed)
	if err != nil {
		// One answer for every failure, as with a password: which part was
		// wrong is not the caller's business.
		return nil, unauthorizedError{}
	}

	result, err := h.sessions.OpenSession(c, user)
	if err != nil {
		return nil, toHTTPError(err)
	}

	request := requestOf(c)
	setCookie(c, sessionCookie(request, result.Token, result.ExpiresAt))

	if rotated := RotateCSRFToken(request); rotated != nil {
		setCookie(c, rotated)
	}

	response := newUserResponseFromModel(result.Principal.User, model.DefaultTimezone)

	return LoginResponse{User: &response, Permissions: permissionsOf(result.Principal)}, nil
}

// parseCreation turns the browser's answer back into what the library parses.
//
// The library reads from an http.Request body, which this handler does not
// have - GoFr already consumed it. Re-encoding the decoded map is the shortest
// way back to a reader it accepts.
func parseCreation(credential map[string]any) (*protocol.ParsedCredentialCreationData, error) {
	body, err := json.Marshal(credential)
	if err != nil {
		return nil, apperror.Invalidf("the credential could not be read")
	}

	parsed, err := protocol.ParseCredentialCreationResponseBody(bytes.NewReader(body))
	if err != nil {
		return nil, apperror.Invalidf("the credential could not be read")
	}

	return parsed, nil
}

func parseAssertion(credential map[string]any) (*protocol.ParsedCredentialAssertionData, error) {
	body, err := json.Marshal(credential)
	if err != nil {
		return nil, apperror.Invalidf("the credential could not be read")
	}

	parsed, err := protocol.ParseCredentialRequestResponseBody(bytes.NewReader(body))
	if err != nil {
		return nil, apperror.Invalidf("the credential could not be read")
	}

	return parsed, nil
}
