package rest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"
	"gofr.dev/pkg/gofr/logging"
)

// recordingLogger keeps what was logged as an error, and passes everything else
// to the logger it wraps.
type recordingLogger struct {
	logging.Logger

	errors []string
}

func (r *recordingLogger) Errorf(format string, args ...any) {
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
}

// A reset whose sessions could not be ended says so.
//
// The reset exists to lock somebody out, and the sessions still open are the
// part of them that would be left. Failing to end those is not a failure of the
// reset - the password is changed, and the answer says so - but it was not
// written anywhere either: the comment above the call said the failure was
// logged by whatever ends the sessions, and nothing on that path logs. An
// administrator wondering why the person is still in had nothing to find.
func TestAResetWhoseSessionsCannotBeEndedSaysSo(t *testing.T) {
	logger := &recordingLogger{Logger: logging.NewMockLogger(logging.ERROR)}
	c := &gofr.Context{
		Context:   context.Background(),
		Container: &container.Container{Logger: logger},
	}

	h := &UserHandler{sessions: NewSessionEnder(func(context.Context, uint) error {
		return errors.New("the database went away")
	})}

	h.endSessionsOf(c, 7)

	if len(logger.errors) != 1 || !strings.Contains(logger.errors[0], "the database went away") {
		t.Errorf("a reset that left the account's sessions open logged %q", logger.errors)
	}
}
