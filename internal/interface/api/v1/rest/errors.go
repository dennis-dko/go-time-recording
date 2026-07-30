// Package rest adapts the application services to HTTP. It owns request
// binding, DTO shaping and status codes; the layers below stay unaware of HTTP.
package rest

import (
	gofrHTTP "gofr.dev/pkg/gofr/http"

	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

// toHTTPError maps an application error onto the GoFr error types, which carry
// the status code GoFr's responder will use. Doing the mapping here is what
// lets the inner layers stay free of HTTP imports.
func toHTTPError(err error) error {
	if err == nil {
		return nil
	}

	detail, ok := apperror.Detail(err)
	if !ok {
		// Not one of ours: let GoFr report it as a 500 rather than guessing.
		return err
	}

	switch detail.Kind {
	case apperror.KindNotFound:
		return gofrHTTP.ErrorEntityNotFound{Name: detail.Entity, Value: detail.ID}
	case apperror.KindInvalid:
		if len(detail.Fields) > 0 {
			return gofrHTTP.ErrorInvalidParam{Params: detail.Fields}
		}

		return gofrHTTP.ErrorInvalidParam{Params: []string{detail.Error()}}
	case apperror.KindConflict:
		// GoFr's ErrorEntityAlreadyExist has a fixed message, which would
		// discard the reason for the conflict, so the detail is carried by a
		// local type that keeps the 409 and the explanation.
		return conflictError{msg: detail.Error()}
	case apperror.KindInternal:
		return err
	default:
		return err
	}
}

// conflictError renders a 409 while preserving the explanation.
type conflictError struct {
	msg string
}

func (e conflictError) Error() string { return e.msg }

// StatusCode is what GoFr's responder looks for to pick the HTTP status.
func (conflictError) StatusCode() int { return 409 }
