package service

import (
	"context"

	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// ExternalDirectory finds somebody in the directory by the name they are known
// by, without their password - for a sign-in whose identity something else has
// already proved, and never a way round one that has not.
type ExternalDirectory interface {
	Lookup(ctx context.Context, login string) (*ExternalUser, bool, error)
}

// KerberosLogin opens a session for the owner of a Kerberos ticket the caller has
// already verified: name, of realm.
//
// The ticket says who this is and nothing more, so everything else is decided as
// a directory password sign-in decides it. The name is looked up in the
// directory - as it is, then as name@realm, which is how Active Directory writes
// a userPrincipalName - and the account is the one provisionExternal finds or
// makes, so either way of signing in reaches the same account, and the built-in
// administrator and anybody who administers are refused as they are there. A
// second factor its owner enrolled is asked for: the ticket stands in for the
// password and for nothing else. Without a directory there is nobody to look the
// name up with, and a name on its own is not an account.
func (s *SessionService) KerberosLogin(ctx context.Context, name, realm, totpCode string) (*LoginResult, error) {
	directory, ok := s.external.(ExternalDirectory)
	if !ok || !s.external.Enabled() {
		return nil, apperror.Conflictf("a Kerberos sign-in is looked up in the directory, and no " +
			"directory is configured").WithCode("kerberosNeedsDirectory")
	}

	directoryUser, err := lookUpPrincipal(ctx, directory, name, realm)
	if err != nil {
		s.count(ctx, MetricSignInFailures, "reason", SignInFailureDirectory)

		return nil, err
	}

	if directoryUser == nil {
		s.count(ctx, MetricSignInFailures, "reason", SignInFailureCredentials)

		// As vague to the caller as any refused sign-in; the message is for the
		// log, where a ticket that verified and reached nobody is most often a
		// user filter that does not match the name tickets carry.
		return nil, apperror.Invalidf("the directory holds nobody named %s or %s@%s", name, name, realm).
			WithCode("invalidCredentials")
	}

	user, err := s.provisionExternal(ctx, directoryUser)
	if err != nil {
		// A refusal counted as the password path counts it: an account the
		// directory has switched off, or one this sign-in may never reach.
		if apperror.KindOf(err) != apperror.KindInternal {
			s.count(ctx, MetricSignInFailures, "reason", SignInFailureCredentials)
		}

		return nil, err
	}

	if err := s.secondFactor(ctx, user, totpCode); err != nil {
		return nil, err
	}

	return s.OpenSession(ctx, user)
}

// lookUpPrincipal finds the directory's entry for name of realm, or nil when the
// directory holds nobody by either form of it.
func lookUpPrincipal(ctx context.Context, directory ExternalDirectory, name, realm string) (*ExternalUser, error) {
	for _, login := range []string{name, name + "@" + realm} {
		entry, found, err := directory.Lookup(ctx, login)
		if err != nil {
			return nil, apperror.Internal(err)
		}

		if found {
			return entry, nil
		}
	}

	return nil, nil
}
