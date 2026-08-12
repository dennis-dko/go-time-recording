package rest

import (
	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
)

// SetupHandler serves the first-run wizard.
type SetupHandler struct {
	setup *service.SetupService
	authz *Authorizer
}

// NewSetupHandler creates the handler.
func NewSetupHandler(setup *service.SetupService, authz *Authorizer) *SetupHandler {
	return &SetupHandler{setup: setup, authz: authz}
}

// State handles GET /api/v1/setup.
//
// Restricted to the built-in administrator, and not because the answer is
// sensitive on its own: it is a list of what has not been configured yet, which
// is a useful thing for an attacker to read - "no directory, still on the
// initial password" is a plan.
func (h *SetupHandler) State(c *gofr.Context) (any, error) {
	if err := h.requireSystemAdmin(c); err != nil {
		return nil, err
	}

	state, err := h.setup.State(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return state, nil
}

// Complete handles POST /api/v1/setup/complete, dismissing the wizard.
//
// The required steps are not re-checked here on purpose. Dismissing is about
// the optional ones, and the wizard comes back by itself while anything
// required is outstanding - so refusing here would only be a second, ruder way
// of saying the same thing.
func (h *SetupHandler) Complete(c *gofr.Context) (any, error) {
	if err := h.requireSystemAdmin(c); err != nil {
		return nil, err
	}

	if err := h.setup.Complete(c); err != nil {
		return nil, toHTTPError(err)
	}

	state, err := h.setup.State(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return state, nil
}

// requireSystemAdmin restricts the wizard to the built-in administrator, which
// is the account that can reach every screen it points at.
func (h *SetupHandler) requireSystemAdmin(c *gofr.Context) error {
	principal, err := h.authz.Principal(c)
	if err != nil {
		return err
	}

	if !h.authz.Enabled() || principal.User.IsSystem {
		return nil
	}

	return forbiddenError{msg: "only the built-in administrator may run the setup wizard"}.
		WithCode("onlyBuiltInAdminSetsUp")
}
