package rest

import (
	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
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
	Aborted string `json:"aborted,omitempty"`
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

	report, err := h.sync.Sync(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	if len(report.Deleted) > 0 {
		c.Logger.Warnf("directory sync removed %d account(s) and their records", len(report.Deleted))
	}

	return newSyncReportResponse(report), nil
}

// requireSystemAdmin restricts the run to the built-in administrator.
func (h *LDAPSyncHandler) requireSystemAdmin(c *gofr.Context) error {
	principal, err := h.authz.Principal(c)
	if err != nil {
		return err
	}

	if !h.authz.Enabled() || principal.User.IsSystem {
		return nil
	}

	return forbiddenError{msg: "only the built-in administrator may synchronise the directory"}.
		WithCode("onlyBuiltInAdminSyncs")
}

func newSyncReportResponse(r *service.SyncReport) SyncReportResponse {
	resp := SyncReportResponse{
		DirectoryUsers: r.DirectoryUsers,
		LocalExternal:  r.LocalExternal,
		DryRun:         r.DryRun,
		Aborted:        r.Aborted,
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
