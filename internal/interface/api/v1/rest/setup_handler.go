package rest

import (
	"crypto/subtle"
	"strings"

	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

// SetupHandler serves the first-run wizard.
type SetupHandler struct {
	setup *service.SetupService
	authz *Authorizer

	// installerToken is the token this process's installer accepted, and is
	// empty on every start that did not run one. Empty means Claim refuses
	// everything - see there for why that is the important half.
	installerToken string

	sessions *service.SessionService
}

// NewSetupHandler creates the handler.
func NewSetupHandler(setup *service.SetupService, authz *Authorizer) *SetupHandler {
	return &SetupHandler{setup: setup, authz: authz}
}

// FromInstaller lets the browser that answered the installer be handed a session
// rather than a password to type.
//
// Only called on a start that actually served an installer, so an installation
// configured from a compose file never gains the endpoint's one working path.
func (h *SetupHandler) FromInstaller(token string, sessions *service.SessionService) *SetupHandler {
	h.installerToken = token
	h.sessions = sessions

	return h
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

// Claim handles POST /api/v1/setup/claim.
//
// The last step of the installer, run against the application that replaced it.
// The browser holding the setup token asks for the session that token earned,
// and lands on the wizard's first screen - choose a password - instead of on a
// sign-in form asking for one out of the documentation.
//
// Three things have to be true, and each one closes a different door.
//
// A token has to have been configured at all. This is the half that an
// ordinary-looking comparison gets wrong: on a deployment that never ran an
// installer there is no token, a request arriving without the header carries an
// empty string, and empty equals empty. So an unset token refuses before any
// comparison happens, and the endpoint has no working path on such an
// installation at all.
//
// The token has to match, in constant time, because it is a secret compared
// against a value somebody supplies.
//
// And the installation must never have been taken into use, which the session
// service decides by whether the built-in administrator still has to choose a
// password. That is the same condition the documented initial password already
// turns on, so this grants nothing that was not already reachable - and it stops
// granting it at the same moment.
//
// Not behind requireSystemAdmin, unlike everything else on this handler: there
// is nobody signed in yet, which is the entire point.
func (h *SetupHandler) Claim(c *gofr.Context) (any, error) {
	if h.installerToken == "" || h.sessions == nil {
		return nil, toHTTPError(apperror.Conflictf(
			"this installation was not configured through the installer").
			WithCode("noInstallerSession"))
	}

	request := requestOf(c)
	if request == nil {
		return nil, toHTTPError(apperror.Conflictf("no request to answer").
			WithCode("noInstallerSession"))
	}

	offered := strings.TrimSpace(request.Header.Get(SetupTokenHeader))
	if subtle.ConstantTimeCompare([]byte(offered), []byte(h.installerToken)) != 1 {
		return nil, unauthorizedError{}
	}

	result, err := h.sessions.OpenFirstSession(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	setCookie(c, sessionCookie(request, result.Token, result.ExpiresAt))

	// The same rotation a sign-in does: a token handed to an anonymous visitor
	// must not follow them into a signed-in session.
	if rotated := RotateCSRFToken(request); rotated != nil {
		setCookie(c, rotated)
	}

	return map[string]bool{"ok": true}, nil
}

// SetupTokenHeader carries the installer's token on the claim above. The same
// header the installer itself reads, because it is the same secret being shown
// to the same process - only after it has become the application.
const SetupTokenHeader = "X-Setup-Token"

// requireSystemAdmin restricts the wizard to the built-in administrator, which
// is the account that can reach every screen it points at.
func (h *SetupHandler) requireSystemAdmin(c *gofr.Context) error {
	principal, err := h.authz.Principal(c)
	if err != nil {
		return err
	}

	if h.authz.AdministersOnly(principal) {
		return nil
	}

	return forbiddenError{msg: "only an administrator of this installation may run " +
		"the setup wizard"}.WithCode("onlyBuiltInAdminSetsUp")
}
