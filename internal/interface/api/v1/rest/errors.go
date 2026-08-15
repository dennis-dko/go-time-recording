// Package rest adapts the application services to HTTP. It owns request
// binding, DTO shaping and status codes; the layers below stay unaware of HTTP.
package rest

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
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
		return newInternalError(detail)
	default:
		return err
	}
}

// newInternalError turns something that went wrong inside into an answer a reader
// can act on.
//
// What used to go out was whatever the failure said: a driver's "dial tcp
// 10.0.0.4:5432: connect: connection refused", a file system's "permission
// denied", the LDAP library's own wording. Every one of them is written in
// English by somebody else's library, and no dictionary here can ever cover them
// - they are not this application's sentences, and the list has no end.
//
// So the answer has three parts, and they are three because they have three
// different audiences. A generic sentence, translated, for the person reading the
// screen. A code, for anything deciding what to do about it. And the original
// text, unchanged, for whoever is going to fix it - kept out of the way behind a
// disclosure rather than thrown at somebody who cannot use it.
//
// The reference ties the three together. It goes into Error(), which is what
// GoFr writes to the log, and into the body, which is what appears on screen - so
// "something went wrong (A7F3C2)" on somebody's screen and the log line holding
// the stack are findable from each other. That is the one thing a generic message
// otherwise costs you.
func newInternalError(detail *apperror.Error) internalError {
	code := detail.Code
	if code == "" {
		code = apperror.CodeInternal
	}

	// The original, which is the wrapped error where there is one and the
	// message where the error was made here.
	original := detail.Error()
	if detail.Err != nil {
		original = detail.Err.Error()
	}

	return internalError{
		reason:   reason{message: "the request could not be completed", code: code},
		ref:      newReference(),
		original: original,
	}
}

// referenceAlphabet has no vowels and no characters that are read as each other.
// This gets read out over a telephone.
const referenceAlphabet = "0123456789ACDEFGHJKLMNPQRTUVWXY"

// newReference is a short identifier for one occurrence.
func newReference() string {
	out := make([]byte, 6)

	for i := range out {
		// Not a secret and not a key: it only has to be different from the last
		// one somebody is looking at. crypto/rand because it is what this
		// application has, and six characters of it is nothing.
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(referenceAlphabet))))
		if err != nil {
			// Cannot happen on any platform this runs on, and a reference is not
			// worth failing a response over.
			return "UNKNOWN"
		}

		out[i] = referenceAlphabet[n.Int64()]
	}

	return string(out)
}

// internalError renders a 500 that says what happened twice: once in a sentence
// anybody can read, and once in the words of whatever actually failed.
type internalError struct {
	reason

	ref      string
	original string
}

// Error is what reaches the log, and it carries the reference and the original
// wording - the log is where the person who can fix this will be looking, and
// nothing about it should be generic.
func (e internalError) Error() string {
	return fmt.Sprintf("%s [%s]: %s", e.message, e.ref, e.original)
}

// StatusCode is what GoFr's responder looks for to pick the HTTP status.
func (internalError) StatusCode() int { return 500 }

// Response is GoFr's ResponseMarshaller.
func (e internalError) Response() map[string]any {
	out := e.response()
	if out == nil {
		out = map[string]any{}
	}

	out["ref"] = e.ref

	// Only when it says something the generic sentence does not. An empty one
	// would put an expander on screen with nothing behind it.
	if e.original != "" {
		out["detail"] = e.original
	}

	return out
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
// the interface says them in the reader's own words.
//
// The other half is whatever the driver said - "connection refused", "password
// authentication failed", "x509: certificate signed by unknown authority". That
// prose cannot be anticipated and cannot be translated: it is written by somebody
// else's library, in English, and the list of possible sentences has no end. It
// used to be shown as it arrived, which on a German screen is the one line nobody
// can read on the one screen where reading it is the whole point.
//
// So it becomes the detail. The reader gets "the connection could not be
// established", in their own language, and the driver's own words are one click
// away for whoever is going to act on them - which, on this screen, is often the
// same person a moment later.
func probeFailure(err error) map[string]any {
	out := map[string]any{"message": err.Error()}

	var detail *apperror.Error
	if !errors.As(err, &detail) {
		// Not one of ours at all: everything it says is the driver's, so all of it
		// is detail and the sentence is the generic one.
		return map[string]any{
			"message": "the connection could not be established",
			"code":    apperror.CodeProbeFailed,
			"detail":  err.Error(),
		}
	}

	if detail.Code != "" {
		out["code"] = detail.Code

		if len(detail.Values) > 0 {
			out["values"] = detail.Values
		}
	} else if len(detail.Fields) == 0 {
		// Ours, but with nothing named - which means the sentence in it came from
		// underneath. A field list is different: that is a complaint this
		// application made about what somebody typed, and it already translates.
		out["message"] = "the connection could not be established"
		out["code"] = apperror.CodeProbeFailed
		out["detail"] = detail.Error()
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
