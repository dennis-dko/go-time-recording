package rest

import (
	"context"
	"errors"
	"net/http"

	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/kerberos"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// kerberosSignIn is the one path a Kerberos ticket is presented on.
const kerberosSignIn = "/api/v1/auth/kerberos"

// kerberosPrincipalKey carries a proved ticket's owner from KerberosTicket to
// AuthHandler.KerberosLogin.
type kerberosPrincipalKey struct{}

// KerberosTicket checks the ticket on a Kerberos sign-in and hands its owner on.
//
// Middleware rather than part of the handler, because the exchange needs the
// response itself: a request without a ticket is answered 401 with the Negotiate
// challenge, which is what makes a browser that trusts this address send one,
// and that is written before a handler could return anything. What follows once
// the ticket is proved - the session, maintenance, the cookie - is the
// handler's, and a password sign-in's. With no keytab it passes the request on
// and the handler says a ticket sign-in is not set up.
func KerberosTicket(acceptor *kerberos.Acceptor) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if acceptor == nil {
			return next
		}

		proved := acceptor.Handler(func(w http.ResponseWriter, r *http.Request, who kerberos.Principal) {
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), kerberosPrincipalKey{}, who)))
		})

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != kerberosSignIn {
				next.ServeHTTP(w, r)

				return
			}

			proved.ServeHTTP(w, r)
		})
	}
}

// mistypedSecondFactor reports a refusal that is the person's rather than the
// installation's.
func mistypedSecondFactor(err error) bool {
	detail, coded := apperror.Detail(err)

	return coded && (detail.Code == "twoFactorCodeInvalid" || detail.Code == "twoFactorCodeUsed")
}

// KerberosState says whether the sign-in screen should try a ticket first.
type KerberosState struct {
	Available bool `json:"available"`
}

// KerberosLoginRequest carries the second factor of an account that holds one.
type KerberosLoginRequest struct {
	TOTP string `json:"totp"`
}

// WithKerberos says when a Kerberos sign-in can succeed here: a keytab was read,
// and a directory is configured to look a ticket's name up in.
func (h *AuthHandler) WithKerberos(available func() bool) *AuthHandler {
	h.kerberos = available

	return h
}

// KerberosSupport handles GET /api/v1/auth/kerberos.
func (h *AuthHandler) KerberosSupport(_ *gofr.Context) (any, error) {
	return KerberosState{Available: h.kerberos != nil && h.kerberos()}, nil
}

// KerberosLogin handles POST /api/v1/auth/kerberos, after KerberosTicket has
// proved whose ticket the request carries.
func (h *AuthHandler) KerberosLogin(c *gofr.Context) (any, error) {
	who, ok := c.Request.Context().Value(kerberosPrincipalKey{}).(kerberos.Principal)
	if !ok {
		return nil, toHTTPError(apperror.Conflictf(
			"signing in with a Kerberos ticket is not set up on this installation").
			WithCode("kerberosOff"))
	}

	var req KerberosLoginRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	result, err := h.sessions.KerberosLogin(c, who.Name, who.Realm, req.TOTP)
	if err != nil {
		if errors.Is(err, service.ErrTOTPRequired) {
			return LoginResponse{TOTPRequired: true}, nil
		}

		// Said, because it is the installation's to fix rather than the person's:
		// a keytab without a directory to look its names up in.
		if detail, coded := apperror.Detail(err); coded && detail.Code == "kerberosNeedsDirectory" {
			return nil, toHTTPError(err)
		}

		switch {
		case apperror.KindOf(err) == apperror.KindInternal:
			c.Logger.Errorf("Kerberos sign-in for %s@%s could not be completed: %v", who.Name, who.Realm, err)
		case !mistypedSecondFactor(err):
			// Unlike a refused password, which is somebody's typo: the ticket was
			// good, so what refused it is the installation - a filter that does not
			// find the name, an account that administers - and the browser falls
			// back to the form with nothing to show anybody why.
			c.Logger.Warnf("Kerberos sign-in for %s@%s refused: %v", who.Name, who.Realm, err)
		}

		return nil, unauthorizedError{}
	}

	return completeSignIn(c, h.sessions, h.maintenance, result, h.timezone.resolve(c))
}
