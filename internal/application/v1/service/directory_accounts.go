package service

import (
	"context"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// provisionExternal returns the local account for a directory user, creating
// it on first sign-in.
//
// The stable identifier is tried first. Falling back to the mail address
// covers accounts created before identifiers were recorded, and adopting the
// identifier on the way through means each account is matched by address at
// most once.
//
// Only "there is no such account" moves on to the next lookup, and in the end to
// creating one. A lookup that failed says nothing about who this is: read as
// absence, a failed lookup by identifier after a renamed mailbox found nothing
// under the new address either and created a second account for the same
// person - an empty one, which they were then signed in to.
func (s *SessionService) provisionExternal(ctx context.Context, directoryUser *ExternalUser) (*model.User, error) {
	// Not for an account the directory has switched off. A password sign-in never
	// arrives here with one - the bind refuses it - but a ticket issued before the
	// account went off stays valid for hours, and the entry is the one thing left
	// that can say so. As vague to the caller as any refused sign-in.
	if directoryUser.Disabled {
		return nil, apperror.Invalidf("the directory has switched the account for %s off",
			directoryUser.Email).WithCode("invalidCredentials")
	}

	email := normalizeEmail(directoryUser.Email)

	if directoryUser.ID != "" {
		existing, err := s.users.GetByExternalID(ctx, directoryUser.ID)
		if err == nil {
			return s.reconcileExternal(ctx, existing, directoryUser, email)
		}

		if apperror.KindOf(err) != apperror.KindNotFound {
			return nil, err
		}
	}

	existing, err := s.users.GetByEmail(ctx, email)
	if err == nil {
		// Found by its address while recording another entry's identifier: the
		// account of whoever held the address before - or of whoever an entry
		// naming that address was written to impersonate. Taken over, the
		// newcomer signed in to the other person's hours. A run reads the same
		// pair as a departure and an arrival, and this agrees with it. Changing
		// the identifier attribute forgets every recorded identifier, so that
		// change does not land here for everybody; an entry that arrives with no
		// identifier says nothing about who it is, and is let through as before.
		if directoryUser.ID != "" && existing.ExternalID != "" && existing.ExternalID != directoryUser.ID {
			return nil, apperror.Conflictf("the account under %s records the directory entry %q, "+
				"and the entry signing in is %q; whose account it is is for an administrator to settle",
				email, existing.ExternalID, directoryUser.ID).
				WithCode("accountOfAnotherEntry", email)
		}

		return s.reconcileExternal(ctx, existing, directoryUser, email)
	}

	if apperror.KindOf(err) != apperror.KindNotFound {
		return nil, err
	}

	// A directory entry must never bring the built-in administrator into
	// existence, or the account meant as the way back in would be one the
	// directory controls.
	if email == SystemUserEmail {
		return nil, apperror.Invalidf("invalid credentials").WithCode("invalidCredentials")
	}

	role, err := roleForArrival(ctx, s.roles, directoryUser.Role, s.defaultRole)
	if err != nil {
		return nil, err
	}

	name := directoryUser.Name
	if name == "" {
		name = email
	}

	// No working times, as a synchronisation creates the same account; see
	// createMissing for what a pinned figure cost.
	return s.users.Save(ctx, &model.User{
		Name:       name,
		Email:      email,
		RoleID:     role.ID,
		IsExternal: true,
		ExternalID: directoryUser.ID,
	})
}

// reconcileExternal keeps the local copy in step with the directory: it adopts
// the stable identifier if it is not stored yet, and follows a renamed mailbox
// instead of treating it as a different person.
func (s *SessionService) reconcileExternal(
	ctx context.Context,
	existing *model.User,
	directoryUser *ExternalUser,
	email string,
) (*model.User, error) {
	// The built-in administrator stays local whatever the directory says. Not
	// reachable through the normal sign-in path, which never consults the
	// directory for it, but a stored identifier could still lead here.
	if existing.IsSystem {
		return nil, apperror.Invalidf("invalid credentials").WithCode("invalidCredentials")
	}

	// Nor may the directory claim any other account that administers this
	// installation.
	//
	// Matching by address is how an installation moves from local passwords to a
	// directory without anybody losing their hours, and that is worth keeping.
	// What it must not carry with it is the installation itself: whoever can
	// write an entry to the directory - a helpdesk account in a company
	// directory is enough - could name an administrator's address and sign in
	// holding everything that administrator holds.
	//
	// The built-in account was refused above for exactly this reason, and it is
	// not the only account that administers. An installation that gives somebody
	// the admin role has decided that person administers it, which is precisely
	// what makes their address worth claiming.
	//
	// Only when the directory is claiming an account rather than signing in to
	// one it already owns: an administrator whose account is directory-backed
	// already goes on as before, because nothing is being taken over.
	changed := false

	if !existing.IsExternal {
		administers, err := s.administers(ctx, existing)
		if err != nil {
			return nil, err
		}

		if administers {
			return nil, apperror.Invalidf("invalid credentials").WithCode("invalidCredentials")
		}

		// Taken over, not only matched: the account is the directory's from here
		// on, and keeps nothing local that a directory account cannot have. Kept,
		// an account an administrator created without a password went on
		// demanding that its owner replace the documented initial password on
		// every sign-in - a password they never had - while that password still
		// opened it, because a local account is checked against its own hash
		// whenever the directory refuses. And a synchronisation, which removes
		// only directory accounts, would have let it outlive its owner leaving.
		existing.IsExternal = true
		existing.MustChangePassword = false
		existing.PasswordHash = ""
		changed = true
	}

	if directoryUser.ID != "" && existing.ExternalID != directoryUser.ID {
		existing.ExternalID = directoryUser.ID
		changed = true
	}

	if email != "" && existing.Email != email {
		existing.Email = email
		changed = true
	}

	if directoryUser.Name != "" && existing.Name != directoryUser.Name {
		existing.Name = directoryUser.Name
		changed = true
	}

	if !changed {
		return existing, nil
	}

	return s.users.Update(ctx, existing)
}

// administers reports whether the account holds rights over the installation
// rather than over a working day; roleAdministers says which rights those are.
//
// A role that is not there counts as no permissions, which is the reading
// principalFor already takes of the same condition: an account pointing at a
// deleted role is valid and powerless. Powerless is also nothing worth claiming,
// so this neither refuses the migration nor gives anything away.
//
// A role that could not be read is not that, and it is an error. principalFor
// may read it as no permissions, because there the reading takes rights away;
// here it is the answer that lets the claim through. Read as "no rights", a
// database failing this one query let the directory adopt a local administrator
// and sign in with everything they hold - the guard opening exactly when
// nothing could be checked.
func (s *SessionService) administers(ctx context.Context, user *model.User) (bool, error) {
	role, err := s.roles.GetByID(ctx, user.RoleID)
	if err != nil {
		if apperror.KindOf(err) == apperror.KindNotFound {
			return false, nil
		}

		return false, err
	}

	return roleAdministers(role), nil
}
