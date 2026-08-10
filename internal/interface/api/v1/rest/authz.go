package rest

import (
	"net/http"

	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

// Authorizer resolves the caller behind a request and checks their rights.
//
// Authorization is enforced per handler rather than by a route table, because
// several rules depend on the resource: reading "my" time entries and reading
// everyone's are different permissions on the same route.
type Authorizer struct {
	auth *service.AuthService

	// open disables enforcement. It exists for running the instance without
	// authentication at all, where there is no caller to check.
	open bool
}

// NewAuthorizer creates an authorizer. When enforce is false every request is
// allowed and the caller is reported as anonymous.
func NewAuthorizer(auth *service.AuthService, enforce bool) *Authorizer {
	return &Authorizer{auth: auth, open: !enforce}
}

// Enabled reports whether authorization is being enforced.
func (a *Authorizer) Enabled() bool { return !a.open }

// forbiddenError renders a 403. GoFr has no error type for it, and reusing a
// 400 or 404 would hide the reason from the client.
type forbiddenError struct {
	msg string
}

func (e forbiddenError) Error() string { return e.msg }

// StatusCode is what GoFr's responder reads to pick the HTTP status.
func (forbiddenError) StatusCode() int { return http.StatusForbidden }

// Principal returns the authenticated caller.
//
// With enforcement off there is no caller, so a synthetic principal holding
// every permission is returned; that keeps handlers free of special cases.
func (a *Authorizer) Principal(c *gofr.Context) (*service.Principal, error) {
	if a.open {
		return &service.Principal{
			User:        &model.User{Name: "anonymous"},
			Permissions: model.AllPermissions(),
		}, nil
	}

	principal, ok := principalFromContext(c.Request.Context())
	if !ok || principal.User == nil {
		return nil, unauthorizedError{}
	}

	return principal, nil
}

// unauthorizedError renders a 401, which tells the UI to show the sign-in
// form. A 403 would instead mean "signed in, but not allowed".
type unauthorizedError struct{}

func (unauthorizedError) Error() string { return "not authenticated" }

// StatusCode is what GoFr's responder reads to pick the HTTP status.
func (unauthorizedError) StatusCode() int { return http.StatusUnauthorized }

// RequireSystemAdmin returns the caller only if they are the built-in
// administrator.
//
// Deliberately not a permission. Everything behind this - the database
// connection, the directory bind, the process log - describes the installation
// rather than the work recorded in it, and a role that could be edited into
// holding it would be a way to grant yourself the installation.
func (a *Authorizer) RequireSystemAdmin(c *gofr.Context) (*service.Principal, error) {
	principal, err := a.Principal(c)
	if err != nil {
		return nil, err
	}

	if a.open || principal.User.IsSystem {
		return principal, nil
	}

	return nil, forbiddenError{msg: "only the built-in administrator may do this"}
}

// Require returns the caller only if they hold the permission.
func (a *Authorizer) Require(c *gofr.Context, permission string) (*service.Principal, error) {
	principal, err := a.permittedPrincipal(c)
	if err != nil {
		return nil, err
	}

	if a.open || principal.Can(permission) {
		return principal, nil
	}

	return nil, forbiddenError{msg: "missing permission: " + permission}
}

// RequireAny returns the caller if they hold at least one of the permissions.
// Used where a broad right implies a narrower one.
func (a *Authorizer) RequireAny(c *gofr.Context, permissions ...string) (*service.Principal, error) {
	principal, err := a.permittedPrincipal(c)
	if err != nil {
		return nil, err
	}

	if a.open {
		return principal, nil
	}

	for _, permission := range permissions {
		if principal.Can(permission) {
			return principal, nil
		}
	}

	return nil, forbiddenError{msg: "missing permission: " + permissions[0]}
}

// permittedPrincipal resolves the caller and refuses anyone still on their
// initial password.
//
// The interface tells users that changes are blocked until they choose a
// password; without this the server did not actually enforce it. The password
// change itself, and reading /me, go through Principal and stay reachable.
func (a *Authorizer) permittedPrincipal(c *gofr.Context) (*service.Principal, error) {
	principal, err := a.Principal(c)
	if err != nil {
		return nil, err
	}

	if !a.open {
		if err := mustChangePassword(principal); err != nil {
			return nil, toHTTPError(err)
		}
	}

	return principal, nil
}

// RequireSelfOr allows the action when the caller is acting on their own
// account, or otherwise holds the wider permission.
func (a *Authorizer) RequireSelfOr(
	c *gofr.Context,
	targetUserID uint,
	permission string,
) (*service.Principal, error) {
	principal, err := a.permittedPrincipal(c)
	if err != nil {
		return nil, err
	}

	if a.open || principal.User.ID == targetUserID || principal.Can(permission) {
		return principal, nil
	}

	return nil, forbiddenError{msg: "missing permission: " + permission}
}

// RequireSelf checks an action nobody may take on anybody else's account, and that
// still takes a right of its own on your own.
//
// Distinct from RequireSelfOr, which has a wider permission that reaches other
// people: there is no such permission here on purpose. A daily target and a daily
// ceiling are time figures, everything to do with time is the person's own, and the
// one account that administers this installation cannot see what those numbers do -
// not the entries, not the balance, not the figures. Setting a number whose effect is
// invisible to you is not administration.
//
// The right is still checked for your own, because the role editor offers it and a
// box that does nothing when ticked is worse than no box.
func (a *Authorizer) RequireSelf(
	c *gofr.Context,
	targetUserID uint,
	permission string,
) (*service.Principal, error) {
	principal, err := a.permittedPrincipal(c)
	if err != nil {
		return nil, err
	}

	if a.open {
		return principal, nil
	}

	if principal.User != nil && principal.User.ID == targetUserID &&
		principal.Can(permission) {
		return principal, nil
	}

	return nil, forbiddenError{msg: "you may only change your own working times"}
}

// scopeUserID narrows a requested user filter to what the caller may see, which is
// themselves.
//
// The filter still exists as a parameter because an installation without
// authentication uses it, and because asking about yourself by id is a reasonable
// thing for a client to do. Asking about anybody else is refused rather than
// quietly answered with your own rows: a list that does not match what was asked
// for is worse than a plain no.
func (a *Authorizer) scopeUserID(principal *service.Principal, requested uint) (uint, error) {
	if a.open {
		return requested, nil
	}

	if !principal.Can(model.PermTimesheetReadOwn) {
		return 0, forbiddenError{msg: "missing permission: " + model.PermTimesheetReadOwn}
	}

	if requested != 0 && requested != principal.User.ID {
		return 0, forbiddenError{msg: "you may only read your own time entries"}
	}

	return principal.User.ID, nil
}

// requireOwner checks a write against a specific user's data.
func (a *Authorizer) requireOwner(principal *service.Principal, ownerID uint) error {
	if a.open {
		return nil
	}

	if principal.Can(model.PermTimesheetWriteOwn) && principal.User.ID == ownerID {
		return nil
	}

	return forbiddenError{msg: "you may only change your own time entries"}
}

// reportScope is whose hours a total covers: the caller's own.
//
// Zero means "no narrowing", which is what an installation without authentication
// gets. Everybody else gets their own id, so a total can never add up hours that
// are not theirs.
//
// The second of two locks, and today the redundant one. A project belongs to one
// person and nobody else may even see it, so a report the caller can open is a report
// over their own project, whose entries are their own by construction - which is why
// removing this narrowing does not make any test fail. It stays because it is the lock
// that states the rule: the first one is about which project is visible, and would go
// on holding while quietly ceasing to be about whose hours are counted the moment two
// people could book on one project again.
func (a *Authorizer) reportScope(principal *service.Principal) uint {
	if a.open {
		return 0
	}

	if principal.User == nil {
		return 0
	}

	return principal.User.ID
}

// requireOwnEntry checks an action against whose entry it is, where the right to
// do it at all is a permission of its own.
//
// Separate from requireOwner, which additionally insists on timesheets:write:own.
// Transferring is gated on timesheets:transfer, so a role holding that and not the
// write right must still be able to transfer its own entries - and must not be able
// to touch anybody else's.
func (a *Authorizer) requireOwnEntry(principal *service.Principal, ownerID uint) error {
	if a.open {
		return nil
	}

	if principal.User != nil && principal.User.ID == ownerID {
		return nil
	}

	return forbiddenError{msg: "you may only change your own time entries"}
}

// mustChangePassword blocks everything except the password change itself
// while an initial password is still in place.
func mustChangePassword(principal *service.Principal) error {
	if principal.User != nil && principal.User.MustChangePassword {
		return apperror.Conflictf("the initial password must be changed before using the application").
			WithCode("initialPasswordPending")
	}

	return nil
}
