package rest

import (
	"strconv"
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// LDAPSyncHandler serves the directory synchronisation.
//
// Restricted to the built-in administrator: a run can delete accounts and the
// hours booked against them.
type LDAPSyncHandler struct {
	sync  *service.LDAPSyncService
	authz *Authorizer
}

// NewLDAPSyncHandler creates the handler.
func NewLDAPSyncHandler(sync *service.LDAPSyncService, authz *Authorizer) *LDAPSyncHandler {
	return &LDAPSyncHandler{sync: sync, authz: authz}
}

// SyncCandidateResponse is one account the directory no longer holds.
type SyncCandidateResponse struct {
	UserID uint   `json:"userId"`
	Name   string `json:"name"`
	Email  string `json:"email"`

	// Timesheets counts the entries that would be destroyed with the account.
	Timesheets int `json:"timesheets"`
}

// SyncReportResponse reports what a run did, or would do.
type SyncReportResponse struct {
	DirectoryUsers int  `json:"directoryUsers"`
	LocalExternal  int  `json:"localExternal"`
	DryRun         bool `json:"dryRun"`

	Candidates []SyncCandidateResponse `json:"candidates"`
	Deleted    []SyncCandidateResponse `json:"deleted"`
	Created    []string                `json:"created"`

	// Aborted carries the reason a guard stopped the run; empty when it ran.
	// AbortCode and AbortValues are the same reason for the screen to translate,
	// as it does the code and values of any refusal.
	Aborted     string `json:"aborted,omitempty"`
	AbortCode   string `json:"abortCode,omitempty"`
	AbortValues []any  `json:"abortValues,omitempty"`
}

// Preview handles POST /api/v1/settings/ldap/sync/preview.
//
// It changes nothing, so an administrator can see exactly which accounts and
// how many recorded entries a run would remove before committing to it.
func (h *LDAPSyncHandler) Preview(c *gofr.Context) (any, error) {
	if err := h.requireSystemAdmin(c); err != nil {
		return nil, err
	}

	report, err := h.sync.Preview(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return newSyncReportResponse(report), nil
}

// Run handles POST /api/v1/settings/ldap/sync.
func (h *LDAPSyncHandler) Run(c *gofr.Context) (any, error) {
	if err := h.requireSystemAdmin(c); err != nil {
		return nil, err
	}

	confirmed, bound, err := confirmedAccounts(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	var report *service.SyncReport

	if bound {
		report, err = h.sync.SyncAsConfirmed(c, confirmed)
	} else {
		report, err = h.sync.Sync(c)
	}

	// Logged before the error is answered, and from the report rather than
	// instead of it. A run that removed accounts and then failed still removed
	// them, irreversibly, and the caller's log line is the only place that says
	// which - so on the failure path it matters more than on the success one.
	for _, removed := range report.Removals() {
		c.Logger.Warn(removed)
	}

	if err != nil {
		return nil, toHTTPError(err)
	}

	return newSyncReportResponse(report), nil
}

// confirmedAccounts reads the accounts a run was confirmed for, and whether it
// was bound to any at all.
//
// The screen always sends them, and "?confirmed=" for a preview that proposed
// nobody - which is a confirmation too: that the run deletes no one. Absent, the
// run is unbound, which is what an API client written before this sends. GoFr's
// Params keeps the two apart, nil for an absent key and one empty value for an
// empty one, and says it does so on purpose. A value that is not an id is
// refused rather than dropped, because dropping it would narrow what the caller
// confirmed into something they did not.
func confirmedAccounts(c *gofr.Context) ([]uint, bool, error) {
	values := c.Params("confirmed")
	if values == nil {
		return nil, false, nil
	}

	ids := make([]uint, 0, len(values))

	for _, value := range values {
		if value == "" {
			continue
		}

		id, err := strconv.ParseUint(value, 10, 0)
		if err != nil || id == 0 {
			return nil, false, apperror.InvalidFields("confirmed")
		}

		ids = append(ids, uint(id))
	}

	return ids, true, nil
}

// requireSystemAdmin restricts the run to an account that administers this
// installation and has no working day of its own.
//
// The built-in one, and anybody holding the admin role - see
// Authorizer.AdministersOnly for why those are the same thing and why the
// combined role is not. It asked for the built-in account alone, which meant an
// installation that had handed its administration to a person still had to sign
// in as the account nobody can attribute to one.
func (h *LDAPSyncHandler) requireSystemAdmin(c *gofr.Context) error {
	principal, err := h.authz.Principal(c)
	if err != nil {
		return err
	}

	if h.authz.AdministersOnly(principal) {
		return nil
	}

	return forbiddenError{msg: "only an administrator of this installation may " +
		"synchronise the directory"}.WithCode("onlyBuiltInAdminSyncs")
}

func newSyncReportResponse(r *service.SyncReport) SyncReportResponse {
	resp := SyncReportResponse{
		DirectoryUsers: r.DirectoryUsers,
		LocalExternal:  r.LocalExternal,
		DryRun:         r.DryRun,
		Aborted:        r.Aborted,
		AbortCode:      r.AbortCode,
		AbortValues:    r.AbortValues,
		Candidates:     candidates(r.Candidates),
		Deleted:        candidates(r.Deleted),
		Created:        r.Created,
	}

	if resp.Created == nil {
		resp.Created = []string{}
	}

	return resp
}

func candidates(in []service.SyncCandidate) []SyncCandidateResponse {
	out := make([]SyncCandidateResponse, 0, len(in))
	for _, c := range in {
		out = append(out, SyncCandidateResponse{
			UserID: c.UserID, Name: c.Name, Email: c.Email, Timesheets: c.Timesheets,
		})
	}

	return out
}

// directoryRunsShown is how many of the latest runs the card lists.
const directoryRunsShown = 20

// DirectoryRunResponse is what one run changed, and nothing about whom.
type DirectoryRunResponse struct {
	RanAt          time.Time `json:"ranAt"`
	Confirmed      bool      `json:"confirmed"`
	Deleted        int       `json:"deleted"`
	EntriesDeleted int       `json:"entriesDeleted"`
	Created        int       `json:"created"`
}

// Runs handles GET /api/v1/settings/ldap/sync/runs: the latest runs that changed
// something, newest first.
//
// The built-in administrator's, as running one is. The answer names nobody, so
// it would harm nobody to show it wider; it stays with the run because it says
// what the run did.
func (h *LDAPSyncHandler) Runs(c *gofr.Context) (any, error) {
	if err := h.requireSystemAdmin(c); err != nil {
		return nil, err
	}

	runs, err := h.sync.Runs(c, directoryRunsShown)
	if err != nil {
		return nil, toHTTPError(err)
	}

	items := make([]DirectoryRunResponse, 0, len(runs))

	for _, run := range runs {
		items = append(items, DirectoryRunResponse{
			RanAt:          run.RanAt.UTC(),
			Confirmed:      run.Confirmed,
			Deleted:        run.Deleted,
			EntriesDeleted: run.EntriesDeleted,
			Created:        run.Created,
		})
	}

	return listResponse[DirectoryRunResponse]{Items: items, TotalCount: uint(len(items))}, nil
}
