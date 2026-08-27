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
//
// It carries the same code and values every other refusal does, for the same
// reason: "missing permission: settings:manage" is a sentence for a log, and the
// person who read it had chosen German. It is also the one refusal the interface
// has to act on rather than only show - a 403 where the screen still offers the
// button means the rights changed underneath somebody.
type forbiddenError struct {
	msg        string
	code       string
	codeValues []any
}

func (e forbiddenError) Error() string { return e.msg }

// WithCode names the reason, mirroring apperror.Error.WithCode so a refusal is
// annotated the same way whichever layer raised it - and so the one test that
// checks every code has a German sentence can find these too.
func (e forbiddenError) WithCode(code string, values ...any) forbiddenError {
	e.code, e.codeValues = code, values

	return e
}

// StatusCode is what GoFr's responder reads to pick the HTTP status.
func (forbiddenError) StatusCode() int { return http.StatusForbidden }

// Response is GoFr's ResponseMarshaller, which merges this into the error body.
func (e forbiddenError) Response() map[string]any {
	return reason{code: e.code, values: e.codeValues}.response()
}

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

// Response names it, like every other refusal. This one had no code for as long
// as it existed, on the reasoning that a 401 is unambiguous - and it is, to
// something reading the status. What reads the body is a person, and they were
// reading "not authenticated" in English on a German screen.
func (unauthorizedError) Response() map[string]any {
	return reason{code: apperror.CodeUnauthenticated}.response()
}

// RequireInstallationAdmin returns the caller only if they may configure the
// installation: the built-in administrator, or somebody holding
// model.PermSettingsManage.
//
// This used to be the built-in account and nothing else, on purpose, and the
// argument for that is worth keeping because it is still true: everything behind
// here - the database connection, the directory bind, the process log - describes
// the installation rather than the work recorded in it, and a role that can be
// edited into holding it is a way to grant yourself the installation.
//
// That is now the arrangement, and the cost is exactly what the old comment
// predicted: roles:write is the installation as well, because whoever holds it can
// tick settings:manage on a role and assign it. Nothing here prevents that, and
// pretending otherwise would be worse than saying it - so it is said here, where
// the next reader meets it.
//
// What made it worth paying: an account that administers the accounts but cannot
// reach the database screen is half an administrator, and the way people worked
// around it was to sign in as the built-in account, which is the one account whose
// actions are hardest to attribute to a person.
func (a *Authorizer) RequireInstallationAdmin(c *gofr.Context) (*service.Principal, error) {
	principal, err := a.Principal(c)
	if err != nil {
		return nil, err
	}

	if a.open || principal.User.IsSystem || principal.Can(model.PermSettingsManage) {
		return principal, nil
	}

	return nil, missingPermission(model.PermSettingsManage)
}

// AdministersOnly reports whether this account administers the installation and
// has no working day of its own.
//
// A handful of things belong to that kind of account rather than to anybody who
// can reach the Settings screen: running or scheduling a directory
// synchronisation, and the setup wizard. They used to ask whether the caller was
// the *built-in* administrator, which is a different question with almost the
// same answer - and the difference is the whole point. An installation that gives
// somebody the admin role has decided that person administers it; making them
// sign in as the built-in account for two of those things is how the one account
// nobody can attribute to a person ends up being the one everybody uses.
//
// Asked as "administers and does not work here" rather than by the role's name.
// The name is a string an installation may have changed, and the property that
// actually matters is the shape: the admin role holds the administration and none
// of the working day, which is exactly what makes its holder equivalent to the
// built-in account. The combined role is deliberately not equivalent - somebody
// who also books time keeps their own screens, their tour and their working
// times, and giving them the directory purge as well was never the intent.
//
// The built-in account is still named, because it is guaranteed to be this even
// if an installation edits its way into an odd state.
func (a *Authorizer) AdministersOnly(principal *service.Principal) bool {
	if a.open {
		return true
	}

	if principal == nil || principal.User == nil {
		return false
	}

	return principal.User.IsSystem ||
		(principal.Can(model.PermSettingsManage) && !principal.Can(model.PermTimesheetWriteOwn))
}

// missingPermission is the one refusal shape, so every 403 the interface meets
// carries the same code and can be acted on rather than only shown.
func missingPermission(permission string) forbiddenError {
	return forbiddenError{msg: "missing permission: " + permission}.
		WithCode("missingPermission", permission)
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

	return nil, missingPermission(permission)
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

	return nil, missingPermission(permission)
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

	return nil, forbiddenError{msg: "you may only change your own working times"}.
		WithCode("onlyOwnWorkingTimes")
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
		return 0, missingPermission(model.PermTimesheetReadOwn)
	}

	if requested != 0 && requested != principal.User.ID {
		return 0, forbiddenError{msg: "you may only read your own time entries"}.
			WithCode("onlyOwnEntriesRead")
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

	return forbiddenError{msg: "you may only change your own time entries"}.
		WithCode("onlyOwnEntriesWrite")
}

// viewerID is whose eyes a read is made through, or 0 when authorization is
// switched off entirely - which the services read as "no narrowing", and which
// is what a local trial with AUTH_ENABLED=false is.
//
// One question asked on nearly every read, so it lives with the rest of the
// answers about the caller. It used to be a method on three handlers, identical
// in name and signature and not quite identical in body: one guarded the
// principal and two dereferenced it. Nothing had gone wrong, because every call
// site follows a successful Require - but that was a property of the callers
// rather than of the copies, and the copies were what a fourth handler would
// have been written from.
//
// The guarded reading is the one that survived. It costs a comparison and
// removes the question.
func (a *Authorizer) viewerID(principal *service.Principal) uint {
	if a.open || principal == nil || principal.User == nil {
		return 0
	}

	return principal.User.ID
}

// reportScope is whose hours a total covers: the caller's own.
//
// Zero means "no narrowing", which is what an installation without authentication
// gets. Everybody else gets their own id, so a total can never add up hours that
// are not theirs.
//
// Deliberately not viewerID above, which today computes the same number. The
// two are the same only for as long as a project belongs to exactly one person,
// and folding them together would quietly make that an assumption nobody
// records rather than a coincidence two locks happen to share.
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

	return forbiddenError{msg: "you may only change your own time entries"}.
		WithCode("onlyOwnEntriesWrite")
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
