// Package rest adapts the application services to HTTP. It owns request
// binding, DTO shaping and status codes; the layers below stay unaware of HTTP.
package rest

import (
	"errors"
	"fmt"
	"strings"

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
			// A local type rather than GoFr's ErrorInvalidParam for a reason worth
			// recording: that one declares a `param` field, but GoFr only merges
			// extra fields for errors implementing its ResponseMarshaller, which it
			// does not - so the field names never left the process and the client
			// was left parsing them back out of "'1' invalid parameter(s): x".
			return invalidFieldsError{fields: detail.Fields}
		}

		// A local type rather than GoFr's ErrorInvalidParam, which carries only
		// the prose - and the prose is the one thing a reader in another language
		// cannot use.
		return invalidError{reason: reasonOf(detail)}
	case apperror.KindConflict:
		// GoFr's ErrorEntityAlreadyExist has a fixed message, which would
		// discard the reason for the conflict, so the detail is carried by a
		// local type that keeps the 409 and the explanation.
		return conflictError{reason: reasonOf(detail)}
	case apperror.KindInternal:
		return err
	default:
		return err
	}
}

// reason is a refusal in a form something other than an English reader can act
// on: the sentence as written, plus the name of the rule and the values it
// interpolated.
//
// The code is what the interface looks up to say the same thing in the reader's
// language; the message is what it falls back to, and what reaches the log either
// way. Values travel separately because the numbers are already inside the
// message by the time it exists, and a translation needs them apart from it to put
// them in its own word order.
type reason struct {
	message string
	code    string
	values  []any
}

func reasonOf(detail *apperror.Error) reason {
	return reason{message: detail.Error(), code: detail.Code, values: detail.Values}
}

// response is what GoFr's ResponseMarshaller merges into the error body, which is
// how the code reaches the client alongside the message.
//
// Nothing is added when there is no code, so an unannotated error keeps exactly
// the body it had.
func (r reason) response() map[string]any {
	if r.code == "" {
		return nil
	}

	out := map[string]any{"code": r.code}
	if len(r.values) > 0 {
		out["values"] = r.values
	}

	return out
}

// probeFailure describes a failed connection test in the shape the interface
// already reads a refusal in.
//
// A probe answers 200 with the reason inside it, because a database that cannot
// be reached is information about the values somebody typed rather than a fault
// in this application. That put the reason outside the path every other refusal
// takes, so it arrived as English prose and was shown as English prose - on the
// one screen where the values being complained about are right there to correct.
//
// Half of what comes back is a fixed complaint: a field left empty, a port that
// is not a number, a dialect nobody has. Those carry a code or a field list and
// the interface says them in the reader's own words. The other half is whatever
// the driver said - "connection refused", "password authentication failed" - and
// that is prose nobody can anticipate, so it travels as the message and is shown
// as it is.
func probeFailure(err error) map[string]any {
	out := map[string]any{"message": err.Error()}

	var detail *apperror.Error
	if !errors.As(err, &detail) {
		return out
	}

	if detail.Code != "" {
		out["code"] = detail.Code

		if len(detail.Values) > 0 {
			out["values"] = detail.Values
		}
	}

	// Named "param" to match what a rejected field is called everywhere else in
	// this API, so one function in the interface can render both.
	if len(detail.Fields) > 0 {
		out["param"] = detail.Fields
	}

	return out
}

// conflictError renders a 409 while preserving the explanation.
type conflictError struct {
	reason
}

func (e conflictError) Error() string { return e.message }

// StatusCode is what GoFr's responder looks for to pick the HTTP status.
func (conflictError) StatusCode() int { return 409 }

// Response is GoFr's ResponseMarshaller: whatever it returns is merged into the
// error object in the body.
func (e conflictError) Response() map[string]any { return e.response() }

// invalidError renders a 400 that says which rule was broken rather than only
// which field.
type invalidError struct {
	reason
}

func (e invalidError) Error() string { return e.message }

// StatusCode is what GoFr's responder looks for to pick the HTTP status.
func (invalidError) StatusCode() int { return 400 }

// Response is GoFr's ResponseMarshaller.
func (e invalidError) Response() map[string]any { return e.response() }

// invalidFieldsError renders a 400 that names the offending fields in the body.
//
// The message is kept in GoFr's wording so nothing that reads the log or the
// message changes; what is new is that the names travel as data, which is the only
// form something can translate.
type invalidFieldsError struct {
	fields []string
}

func (e invalidFieldsError) Error() string {
	return fmt.Sprintf("'%d' invalid parameter(s): %s", len(e.fields), strings.Join(e.fields, ", "))
}

// StatusCode is what GoFr's responder looks for to pick the HTTP status.
func (invalidFieldsError) StatusCode() int { return 400 }

// Response is GoFr's ResponseMarshaller.
func (e invalidFieldsError) Response() map[string]any {
	return map[string]any{"param": e.fields}
}
