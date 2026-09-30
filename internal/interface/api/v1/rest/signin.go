package rest

import (
	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// completeSignIn hands a caller the session a sign-in opened - the cookie and a
// fresh CSRF token - unless the installation is out of service for them.
//
// Every way of signing in ends here, because what happens once somebody is known
// does not depend on how they proved it: a password, a passkey, a Kerberos ticket.
// It was written into the password sign-in alone, and a passkey sign-in during
// maintenance opened the session the password one would have ended, and answered
// 200. zone is the one the user in the answer is described in.
func completeSignIn(
	c *gofr.Context,
	sessions *service.SessionService,
	maintenance MaintenanceState,
	result *service.LoginResult,
	zone string,
) (any, error) {
	// Out of service, and not for somebody who could end it.
	//
	// After the credentials are checked rather than before, because "who is
	// this" is the question being answered. It costs a session that is created
	// and immediately ended, which is the price of not having a second way to
	// resolve an account - one that would be a second answer to "who proved
	// this", kept in step with the first by nothing.
	//
	// Ended rather than left to expire: an unused session is still a session,
	// and one handed out during maintenance would let its holder back in the
	// moment maintenance ended, without signing in.
	if turnedAway, notice := refusedByMaintenance(c, maintenance, result.Principal); turnedAway {
		if err := sessions.Logout(c, result.Token); err != nil {
			c.Logger.Errorf("could not end the session refused by maintenance: %v", err)
		}

		return nil, notice
	}

	request := requestOf(c)
	setCookie(c, sessionCookie(request, result.Token, result.ExpiresAt))

	// A token handed to an anonymous visitor must not follow them into a
	// signed-in session: if someone else planted the one they arrived with,
	// they would know the value protecting the new session.
	if rotated := RotateCSRFToken(request); rotated != nil {
		setCookie(c, rotated)
	}

	user := newUserResponseFromModel(result.Principal.User, zone)

	return LoginResponse{
		User:        &user,
		Permissions: permissionsOf(result.Principal),
	}, nil
}

// refusedByMaintenance reports whether this account is turned away because the
// installation is out of service, and the refusal to send if it is.
//
// The same rule the middleware applies to every other request: the built-in
// account and anybody holding settings:manage get in, because the only way out
// of maintenance mode is through a screen they are the only ones who can reach.
// Everybody else is told why, which is the part that was missing - sign-in was
// exempt as a whole, so an ordinary account signed in successfully and then met
// a wall of 503s on a screen that had already welcomed them.
func refusedByMaintenance(
	c *gofr.Context, maintenance MaintenanceState, principal *service.Principal,
) (bool, error) {
	if maintenance == nil || principal == nil || principal.User == nil {
		return false, nil
	}

	state := maintenance.State(c)
	if !state.Enabled {
		return false, nil
	}

	if principal.User.IsSystem || principal.Can(model.PermSettingsManage) {
		return false, nil
	}

	return true, maintenanceError{state: state}
}
