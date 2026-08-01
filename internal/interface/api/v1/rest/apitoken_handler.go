package rest

import (
	"context"
	"net/http"
	"strings"
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
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
				// answer with 401, which keeps one place deciding that.
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
	ID         uint    `json:"id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"prefix"`
	CreatedAt  Date    `json:"createdAt"`
	ExpiresAt  *Date   `json:"expiresAt"`
	LastUsedAt *Date   `json:"lastUsedAt"`
	Expired    bool    `json:"expired"`
	Secret     *string `json:"secret,omitempty"`
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

	return newAPITokenResponse(issued.Token, &issued.Secret), nil
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
		CreatedAt: Date{Time: token.CreatedAt},
		Expired:   token.Expired(time.Now()),
		Secret:    secret,
	}

	if token.ExpiresAt != nil {
		resp.ExpiresAt = &Date{Time: *token.ExpiresAt}
	}

	if token.LastUsedAt != nil {
		resp.LastUsedAt = &Date{Time: *token.LastUsedAt}
	}

	return resp
}
