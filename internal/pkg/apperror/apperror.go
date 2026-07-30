// Package apperror defines transport-agnostic failure kinds shared by the
// domain and application layers. Handlers map a Kind onto a protocol status
// code, which keeps HTTP concerns out of the inner layers.
package apperror

import (
	"errors"
	"fmt"
	"strings"
)

// Kind classifies a failure coarsely enough for a transport to pick a status
// code, without leaking the specific reason into the transport layer.
type Kind uint8

const (
	// KindInternal is the zero value so that a plain error, or an Error built
	// without an explicit kind, is never mistaken for a client-side problem.
	KindInternal Kind = iota
	KindNotFound
	KindInvalid
	KindConflict
)

// Error carries a Kind plus enough structured detail for a transport to build
// a useful message without re-parsing prose.
type Error struct {
	Kind   Kind
	Entity string   // e.g. "timesheet"
	ID     string   // identifier that was looked up, when relevant
	Fields []string // offending field names, for KindInvalid
	Msg    string
	Err    error
}

func (e *Error) Error() string {
	switch {
	case e.Msg != "":
		return e.Msg
	case e.Kind == KindNotFound && e.Entity != "":
		return fmt.Sprintf("%s with id %s not found", e.Entity, e.ID)
	case e.Kind == KindInvalid && len(e.Fields) > 0:
		return "invalid field(s): " + strings.Join(e.Fields, ", ")
	case e.Err != nil:
		return e.Err.Error()
	default:
		return "internal error"
	}
}

func (e *Error) Unwrap() error { return e.Err }

// NotFound reports that entity with the given id does not exist.
func NotFound(entity, id string) *Error {
	return &Error{Kind: KindNotFound, Entity: entity, ID: id}
}

// Invalidf reports client input that failed validation.
func Invalidf(format string, a ...any) *Error {
	return &Error{Kind: KindInvalid, Msg: fmt.Sprintf(format, a...)}
}

// InvalidFields reports specific fields that failed validation.
func InvalidFields(fields ...string) *Error {
	return &Error{Kind: KindInvalid, Fields: fields}
}

// Conflictf reports a request that clashes with the current state, such as a
// duplicate key or an illegal state transition.
func Conflictf(format string, a ...any) *Error {
	return &Error{Kind: KindConflict, Msg: fmt.Sprintf(format, a...)}
}

// Internal wraps an unexpected failure that the caller cannot act on.
func Internal(err error) *Error {
	return &Error{Kind: KindInternal, Err: err}
}

// KindOf reports the Kind of the first *Error in err's chain, defaulting to
// KindInternal so unclassified errors are never treated as the client's fault.
func KindOf(err error) Kind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}

	return KindInternal
}

// Detail returns the first *Error in err's chain, if any.
func Detail(err error) (*Error, bool) {
	var e *Error
	ok := errors.As(err, &e)

	return e, ok
}
