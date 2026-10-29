package rest

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
	"github.com/dennis-dko/go-time-recording/internal/support/qrcode"
)

// APITokenHeader is the alternative to "Authorization: Bearer <token>", for
// clients where the Authorization header is already taken.
const APITokenHeader = "X-API-Token" //nolint:gosec // header name, not a credential

// APITokenMiddleware authenticates requests that carry a personal token.
//
// It runs after the session middleware and only fills in a principal when
// none is there yet, so a browser session always wins over a stray header.
func APITokenMiddleware(tokens *service.APITokenService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, alreadySignedIn := principalFromContext(r.Context()); alreadySignedIn {
				next.ServeHTTP(w, r)

				return
			}

			secret := presentedToken(r)
			if secret == "" {
				next.ServeHTTP(w, r)

				return
			}

			principal, err := tokens.Resolve(r.Context(), secret)
			if err != nil {
				// Left anonymous rather than rejected here: the handlers
				// answer with 401, which keeps one place deciding that. Told,
				// where the token could not be looked up, so that the answer is
				// that failure rather than an invalid token.
				if apperror.KindOf(err) == apperror.KindInternal {
					r = r.WithContext(context.WithValue(r.Context(), uncheckedCallerKey{}, err))
				}

				next.ServeHTTP(w, r)

				return
			}

			ctx := context.WithValue(r.Context(), sessionContextKey{}, principal)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// presentedToken reads the token from either accepted header.
func presentedToken(r *http.Request) string {
	if header := r.Header.Get(APITokenHeader); header != "" {
		return strings.TrimSpace(header)
	}

	const bearer = "Bearer "
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, bearer) {
		return strings.TrimSpace(strings.TrimPrefix(auth, bearer))
	}

	return ""
}

// APITokenHandler serves the caller's own tokens.
type APITokenHandler struct {
	tokens *service.APITokenService
	authz  *Authorizer
}

// NewAPITokenHandler creates the handler.
func NewAPITokenHandler(tokens *service.APITokenService, authz *Authorizer) *APITokenHandler {
	return &APITokenHandler{tokens: tokens, authz: authz}
}

// APITokenResponse describes a token without its secret.
type APITokenResponse struct {
	ID     uint   `json:"id"`
	Name   string `json:"name"`
	Prefix string `json:"prefix"`

	// Moments rather than this package's Date, which formats a day and throws the
	// rest away. Right for the day a booking belongs to, wrong for a credential:
	// "last used" is the field somebody reads when they think a token has been
	// taken, and a day tells them nothing they did not already know. A token used
	// at three in the morning read exactly like one used at three in the
	// afternoon, and one used twice today read as used once.
	//
	// RFC 3339 in UTC, as stored. A client that parsed these strictly as dates has
	// to be updated; that is a deliberate change to the contract, made because the
	// field was answering the wrong question.
	CreatedAt  time.Time  `json:"createdAt"`
	ExpiresAt  *time.Time `json:"expiresAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`

	Expired bool    `json:"expired"`
	Secret  *string `json:"secret,omitempty"`

	// ConnectURI is what an app reaches this installation with as the token's
	// owner: go-time-recording://connect?url=...&token=..., carrying the two
	// values the terminal client reads as GTR_URL and GTR_TOKEN. QR is the same
	// address as a code to scan, an SVG data URI. Both carry the secret, so both
	// exist where it does - in the answer that created the token - and nowhere
	// else.
	ConnectURI *string `json:"connectUri,omitempty"`
	QR         *string `json:"qr,omitempty"`
}

// connectScheme is the scheme of a connection address, which an app registers
// so that scanning the code with the camera opens it.
const connectScheme = "go-time-recording"

// connectURI is the connection address of an installation reached at address,
// as the owner of token.
func connectURI(address, token string) string {
	return connectScheme + "://connect?" + url.Values{"url": {address}, "token": {token}}.Encode()
}

// CreateAPITokenRequest asks for a new token.
type CreateAPITokenRequest struct {
	Name string `json:"name"`

	// ExpiresInDays of 0 means the token does not expire on its own.
	ExpiresInDays int `json:"expiresInDays"`
}

// List handles GET /api/v1/me/tokens.
func (h *APITokenHandler) List(c *gofr.Context) (any, error) {
	principal, err := h.authz.Principal(c)
	if err != nil {
		return nil, err
	}

	tokens, err := h.tokens.List(c, principal.User.ID)
	if err != nil {
		return nil, toHTTPError(err)
	}

	items := make([]APITokenResponse, 0, len(tokens))
	for _, token := range tokens {
		items = append(items, newAPITokenResponse(token, nil))
	}

	return listResponse[APITokenResponse]{Items: items, TotalCount: uint(len(items))}, nil
}

// Create handles POST /api/v1/me/tokens.
//
// The secret is in this response and nowhere else, ever again.
func (h *APITokenHandler) Create(c *gofr.Context) (any, error) {
	principal, err := h.authz.Principal(c)
	if err != nil {
		return nil, err
	}

	// A token inherits the owner's rights, so an account that still has to
	// change its password must not be able to mint one.
	if err := mustChangePassword(principal); err != nil {
		return nil, toHTTPError(err)
	}

	var req CreateAPITokenRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	issued, err := h.tokens.Create(c, principal.User.ID, req.Name, req.ExpiresInDays)
	if err != nil {
		return nil, toHTTPError(err)
	}

	created := newAPITokenResponse(issued.Token, &issued.Secret)

	// The installation as the page asking sees it, which is the address its owner
	// types into the browser - and so the one an app is to use as well.
	if request := requestOf(c); request != nil {
		connect := connectURI(addressAsSeen(request), issued.Secret)
		created.ConnectURI = &connect

		// A code that failed to render must not take the token down with it: the
		// value and the address are in the answer either way.
		if code, err := qrcode.SVGDataURI(connect); err == nil {
			created.QR = &code
		} else {
			c.Logger.Errorf("could not render the token's QR code: %v", err)
		}
	}

	return created, nil
}

// Revoke handles DELETE /api/v1/me/tokens/{id}.
func (h *APITokenHandler) Revoke(c *gofr.Context) (any, error) {
	principal, err := h.authz.Principal(c)
	if err != nil {
		return nil, err
	}

	id, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	if err := h.tokens.Revoke(c, id, principal.User.ID); err != nil {
		return nil, toHTTPError(err)
	}

	return map[string]string{"status": "revoked"}, nil
}

func newAPITokenResponse(token *model.APIToken, secret *string) APITokenResponse {
	resp := APITokenResponse{
		ID:        token.ID,
		Name:      token.Name,
		Prefix:    token.Prefix,
		CreatedAt: token.CreatedAt,
		Expired:   token.Expired(time.Now()),
		Secret:    secret,
	}

	if token.ExpiresAt != nil {
		resp.ExpiresAt = momentOrNil(*token.ExpiresAt)
	}

	if token.LastUsedAt != nil {
		resp.LastUsedAt = momentOrNil(*token.LastUsedAt)
	}

	return resp
}
