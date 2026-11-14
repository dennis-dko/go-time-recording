package service

import (
	"context"
	"slices"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// roleForArrival is the role an account the directory brings in starts with.
//
// One function for both ways an account arrives, a first sign-in and a
// synchronisation, because each used to decide this for itself and both decided
// it the same wrong way: the everyday role fixed at start-up, whatever the
// directory settings said.
//
// The directory's default role where it names a role that exists and does not
// administer the installation; fallback otherwise. A role that has gone since it
// was chosen is not a reason to turn somebody away at the door, and a role that
// administers is never handed out this way - sign-in refuses to let the directory
// claim an administering account, and a default that administers would give every
// entry anybody writes to the directory the installation itself. The settings
// refuse to store one; this catches a role given those rights afterwards.
func roleForArrival(
	ctx context.Context,
	roles repository.RoleRepository,
	configured, fallback string,
) (*model.Role, error) {
	if configured != "" && configured != fallback {
		role, err := roles.GetByName(ctx, configured)

		switch {
		case err == nil && !roleAdministers(role):
			return role, nil
		case err != nil && apperror.KindOf(err) != apperror.KindNotFound:
			return nil, err
		}
	}

	return roles.GetByName(ctx, fallback)
}

// roleAdministers reports whether a role holds rights over the installation
// rather than over a working day: any of model.AdministeringPermissions.
//
// All three are asked about, because they are the same right a step or two
// apart: settings:manage is the installation, roles:write can tick it on a role,
// and users:write can hand that role - or the admin role itself - to its own
// account. Guarding one without the others leaves the door beside the one that
// was locked, and users:write was that door until 2026-11-14.
func roleAdministers(role *model.Role) bool {
	return slices.ContainsFunc(model.AdministeringPermissions(), func(right string) bool {
		return slices.Contains(role.Permissions, right)
	})
}
