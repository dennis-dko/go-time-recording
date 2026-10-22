package rest

import (
	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// TeamHandler serves managers: whether the installation has them, who looks after
// whom, and what each side of an assignment is shown about it.
type TeamHandler struct {
	team  *service.TeamService
	authz *Authorizer
}

// NewTeamHandler creates the handler.
func NewTeamHandler(team *service.TeamService, authz *Authorizer) *TeamHandler {
	return &TeamHandler{team: team, authz: authz}
}

// ManagersSwitch is whether the installation has managers.
type ManagersSwitch struct {
	Enabled bool `json:"enabled"`
}

// AssignedManagers is the accounts that look after one account.
type AssignedManagers struct {
	ManagerIDs []uint `json:"managerIds"`
}

// TeamMemberResponse is somebody on the other end of an assignment, and the team
// rights that reach across it.
type TeamMemberResponse struct {
	ID     uint     `json:"id"`
	Name   string   `json:"name"`
	Rights []string `json:"rights"`
}

// TeamResponse is the accounts somebody looks after, or the accounts that look
// after them.
type TeamResponse struct {
	Items []TeamMemberResponse `json:"items"`
}

// Switch handles GET /api/v1/settings/managers.
func (h *TeamHandler) Switch(c *gofr.Context) (any, error) {
	if _, err := h.authz.RequireInstallationAdmin(c); err != nil {
		return nil, err
	}

	enabled, err := h.team.Enabled(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return ManagersSwitch{Enabled: enabled}, nil
}

// SaveSwitch handles PUT /api/v1/settings/managers.
//
// The installation's to decide, like everything else under Settings: whether
// anybody here may see somebody else's time at all is a question about how the
// installation is run, and it is shipped answered no.
func (h *TeamHandler) SaveSwitch(c *gofr.Context) (any, error) {
	if _, err := h.authz.RequireInstallationAdmin(c); err != nil {
		return nil, err
	}

	var req ManagersSwitch
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	if err := h.team.SetEnabled(c, req.Enabled); err != nil {
		return nil, toHTTPError(err)
	}

	return req, nil
}

// Managers handles GET /api/v1/users/{id}/managers: the assignment as it is
// stored, whether or not the installation has managers switched on, so that an
// administrator can see what switching them on would reach.
func (h *TeamHandler) Managers(c *gofr.Context) (any, error) {
	if _, err := h.authz.Require(c, model.PermUserRead); err != nil {
		return nil, err
	}

	id, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	managers, err := h.team.ManagerIDsOf(c, id)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return AssignedManagers{ManagerIDs: managers}, nil
}

// SetManagers handles PUT /api/v1/users/{id}/managers, replacing whoever looked
// after the account with the accounts named.
//
// Only an administrator assigns. A manager choosing whose time they see would be
// a manager deciding what they are allowed to know, which is the one decision this
// is built to keep from them.
func (h *TeamHandler) SetManagers(c *gofr.Context) (any, error) {
	if _, err := h.authz.Require(c, model.PermUserWrite); err != nil {
		return nil, err
	}

	id, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	var req AssignedManagers
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	if err := h.team.SetManagers(c, id, req.ManagerIDs); err != nil {
		return nil, toHTTPError(err)
	}

	managers, err := h.team.ManagerIDsOf(c, id)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return AssignedManagers{ManagerIDs: managers}, nil
}

// Team handles GET /api/v1/me/team: the accounts the caller looks after, and with
// which rights - nobody while the installation has no managers.
func (h *TeamHandler) Team(c *gofr.Context) (any, error) {
	principal, err := h.authz.permittedPrincipal(c)
	if err != nil {
		return nil, err
	}

	team, err := h.team.Team(c, principal)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return newTeamResponse(team), nil
}

// MyManagers handles GET /api/v1/me/managers: who may see the caller's time, and
// what each of them may do with it.
//
// Every account can ask, and the answer is never withheld. Somebody whose hours a
// manager reads is told so, by name, before they ever have to wonder - which is
// what makes it a team and not surveillance.
func (h *TeamHandler) MyManagers(c *gofr.Context) (any, error) {
	principal, err := h.authz.Principal(c)
	if err != nil {
		return nil, err
	}

	managers, err := h.team.ManagersOf(c, principal.User.ID)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return newTeamResponse(managers), nil
}

func newTeamResponse(members []service.TeamMember) TeamResponse {
	response := TeamResponse{Items: make([]TeamMemberResponse, 0, len(members))}

	for _, member := range members {
		response.Items = append(response.Items, TeamMemberResponse{
			ID: member.ID, Name: member.Name, Rights: member.Rights,
		})
	}

	return response
}
