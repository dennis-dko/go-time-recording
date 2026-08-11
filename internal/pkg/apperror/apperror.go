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

	// Code names the reason in a way something other than an English reader can
	// act on, and Values carries what the message interpolated.
	//
	// Msg is written in English at the point the rule is enforced, which is right
	// for a log and wrong for the person who tripped over it. The rule that made
	// this obvious is long gone - back when timesheets were submitted and approved,
	// the interface showed a German reader "an approved timesheet can no longer be
	// edited" whatever language they had chosen - but every rule since has the same
	// problem. Translating the prose is not possible - by the time it exists the
	// numbers are already in it - so the reason travels as a code with its values
	// beside it, and whoever displays it looks the sentence up in the reader's own
	// language.
	//
	// Optional. Without a code the English message is shown, which is what happened
	// everywhere before and is still better than nothing.
	Code   string
	Values []any
}

// WithCode names the reason, and records the values the message interpolated so a
// translation can put them back in its own word order.
//
// Returns the same error so it can be attached where the error is built:
//
//	apperror.Conflictf("cannot delete a project that still has %d time entries", n).
//		WithCode("projectHasEntries", n)
func (e *Error) WithCode(code string, values ...any) *Error {
	e.Code, e.Values = code, values

	return e
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
